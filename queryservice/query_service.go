package queryservice

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

// ErrInvalidOrStaleView is returned when attempting to use wrong, stale, or cancelled view.
var ErrInvalidOrStaleView = errors.New("invalid or stale view")

type (
	// QueryService is a gRPC service that implements the QueryServiceServer interface.
	QueryService struct {
		protoqueryservice.UnimplementedQueryServiceServer
		batcher viewsBatcher
		config  *Config
		metrics *perfMetrics
		ready   *channel.Ready
	}
)

// NewQueryService create a new QueryService given a configuration.
func NewQueryService(config *Config) *QueryService {
	return &QueryService{
		config:  config,
		metrics: newQueryServiceMetrics(),
		ready:   channel.NewReady(),
	}
}

// WaitForReady waits for the service resources to initialize, so it is ready to answers requests.
// If the context ended before the service is ready, returns false.
func (q *QueryService) WaitForReady(ctx context.Context) bool {
	return q.ready.WaitForReady(ctx)
}

// Run starts the Prometheus server.
func (q *QueryService) Run(ctx context.Context) error {
	pool, poolErr := vcservice.NewDatabasePool(ctx, q.config.Database)
	if poolErr != nil {
		return poolErr
	}
	defer pool.Close()

	q.batcher = viewsBatcher{
		ctx:     ctx,
		config:  q.config,
		metrics: q.metrics,
		pool:    pool,
		nonConsistentBatcher: batcher{
			ctx: ctx,
			cancel: func() {
			},
			config:   q.config,
			metrics:  q.metrics,
			queryObj: &sharedPool{pool: pool},
		},
	}
	q.ready.SignalReady()

	_ = q.metrics.StartPrometheusServer(ctx, q.config.Monitoring.Server)
	// We don't use the error here as we avoid stopping the service due to monitoring error.
	<-ctx.Done()
	return nil
}

// BeginView implements the query-service interface.
func (q *QueryService) BeginView(
	ctx context.Context, params *protoqueryservice.ViewParameters,
) (*protoqueryservice.View, error) {
	q.metrics.requests.WithLabelValues(grpcBeginView).Inc()
	defer q.requestLatency(grpcBeginView, time.Now())

	if params.TimeoutMilliseconds == 0 ||
		int64(params.TimeoutMilliseconds) > q.config.MaxViewTimeout.Milliseconds() { // nolint:gosec
		params.TimeoutMilliseconds = uint64(q.config.MaxViewTimeout.Milliseconds()) // nolint:gosec
	}

	// We try again if we have view-id collision.
	for ctx.Err() == nil {
		viewID, err := getUUID()
		if err != nil {
			return nil, err
		}
		if q.batcher.makeView(viewID, params) {
			return &protoqueryservice.View{Id: viewID}, nil
		}
	}
	return nil, ctx.Err()
}

// EndView implements the query-service interface.
func (q *QueryService) EndView(
	_ context.Context, view *protoqueryservice.View,
) (*protoqueryservice.View, error) {
	q.metrics.requests.WithLabelValues(grpcEndView).Inc()
	defer q.requestLatency(grpcEndView, time.Now())
	return view, q.batcher.removeViewID(view.Id)
}

// GetRows implements the query-service interface.
func (q *QueryService) GetRows(
	ctx context.Context, query *protoqueryservice.Query,
) (*protoqueryservice.Rows, error) {
	q.metrics.requests.WithLabelValues(grpcGetRows).Inc()
	defer q.requestLatency(grpcGetRows, time.Now())
	for _, ns := range query.Namespaces {
		promutil.AddToCounter(q.metrics.keysRequested, len(ns.Keys))
	}

	batches, err := q.assignRequest(ctx, query)
	if err != nil {
		return nil, err
	}

	res := &protoqueryservice.Rows{
		Namespaces: make([]*protoqueryservice.RowsNamespace, len(query.Namespaces)),
	}
	for i, ns := range query.Namespaces {
		resRows, resErr := batches[i].waitForRows(ctx, ns.Keys)
		if resErr != nil {
			return nil, resErr
		}
		res.Namespaces[i] = &protoqueryservice.RowsNamespace{
			NsId: ns.NsId,
			Rows: resRows,
		}
		promutil.AddToCounter(q.metrics.keysResponded, len(resRows))
	}
	return res, err
}

// GetNamespacePolicies implements the query-service interface.
func (q *QueryService) GetNamespacePolicies(
	ctx context.Context,
	_ *protoqueryservice.Empty,
) (*protoblocktx.NamespacePolicies, error) {
	return queryPolicies(ctx, q.batcher.pool)
}

// GetConfigTransaction implements the query-service interface.
func (q *QueryService) GetConfigTransaction(
	ctx context.Context,
	_ *protoqueryservice.Empty,
) (*protoblocktx.ConfigTransaction, error) {
	return queryConfig(ctx, q.batcher.pool)
}

func (q *QueryService) assignRequest(
	ctx context.Context, query *protoqueryservice.Query,
) ([]*namespaceQueryBatch, error) {
	defer func(start time.Time) {
		promutil.Observe(q.metrics.requestAssignmentLatencySeconds, time.Since(start))
	}(time.Now())
	batcher, err := q.batcher.getBatcher(ctx, query.View)
	if err != nil {
		return nil, err
	}

	batches := make([]*namespaceQueryBatch, len(query.Namespaces))
	for i, ns := range query.Namespaces {
		batches[i], err = batcher.addNamespaceKeys(ctx, ns.NsId, ns.Keys)
		if err != nil {
			return nil, err
		}
	}
	return batches, nil
}

func getUUID() (string, error) {
	uuidObj, err := uuid.NewRandomFromReader(rand.Reader)
	if err != nil {
		return "", err
	}
	return uuidObj.String(), nil
}

func (q *QueryService) requestLatency(method string, start time.Time) {
	promutil.Observe(q.metrics.requestsLatency.WithLabelValues(method), time.Since(start))
}
