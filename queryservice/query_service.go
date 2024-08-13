package queryservice

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

var (
	logger = logging.New("query service")

	// ErrInvalidOrStaleView is returned when attempting to use wrong, stale, or cancelled view.
	ErrInvalidOrStaleView = errors.New("invalid or stale view")
)

type (
	// QueryService is a gRPC service that implements the QueryServiceServer interface.
	QueryService struct {
		protoqueryservice.UnimplementedQueryServiceServer
		viewsBatcher
	}
)

// NewQueryService create a new QueryService given a configuration.
func NewQueryService(ctx context.Context, config *Config) (*QueryService, error) {
	metrics := newQueryServiceMetrics()
	pool, poolErr := vcservice.NewDatabasePool(config.Database)
	if poolErr != nil {
		return nil, poolErr
	}

	serviceCtx, serviceCancel := context.WithCancel(ctx)
	context.AfterFunc(serviceCtx, func() {
		pool.Close()
		if stopErr := metrics.StopServer(); stopErr != nil {
			logger.Errorf("Failed stopping prometheus server: %s", stopErr)
		}
	})

	return &QueryService{
		viewsBatcher: viewsBatcher{
			ctx:     serviceCtx,
			cancel:  serviceCancel,
			config:  config,
			metrics: metrics,
			pool:    pool,
			nonConsistentBatcher: batcher{
				ctx: serviceCtx,
				cancel: func() {
				},
				config:   config,
				metrics:  metrics,
				queryObj: &sharedPool{pool: pool},
			},
		},
	}, nil
}

// Close releases the QueryService resources.
func (q *QueryService) Close() {
	q.cancel()
}

// StartPrometheusServer see Provider.StartPrometheusServer().
func (q *QueryService) StartPrometheusServer() <-chan error {
	return q.metrics.StartPrometheusServer(q.config.Monitoring.Metrics.Endpoint)
}

// BeginView implements the query-service interface.
func (q *QueryService) BeginView(
	ctx context.Context, params *protoqueryservice.ViewParameters,
) (*protoqueryservice.View, error) {
	q.metrics.requests.WithLabelValues(grpcBeginView).Inc()
	defer q.requestLatency(grpcBeginView, time.Now())

	if params.TimeoutMilliseconds == 0 || int64(params.TimeoutMilliseconds) > q.config.MaxViewTimeout.Milliseconds() {
		params.TimeoutMilliseconds = uint64(q.config.MaxViewTimeout.Milliseconds())
	}

	// We try again if we have view-id collision.
	for ctx.Err() == nil {
		viewID, err := getUUID()
		if err != nil {
			return nil, err
		}
		if q.makeView(viewID, params) {
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
	return view, q.removeViewID(view.Id)
}

// GetRows implements the query-service interface.
func (q *QueryService) GetRows(
	ctx context.Context, query *protoqueryservice.Query,
) (*protoqueryservice.Rows, error) {
	q.metrics.requests.WithLabelValues(grpcGetRows).Inc()
	defer q.requestLatency(grpcGetRows, time.Now())
	for _, ns := range query.Namespaces {
		prometheusmetrics.AddToCounter(q.metrics.keysRequested, len(ns.Keys))
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
		prometheusmetrics.AddToCounter(q.metrics.keysResponded, len(resRows))
	}
	return res, err
}

func (q *QueryService) assignRequest(
	ctx context.Context, query *protoqueryservice.Query,
) ([]*namespaceQueryBatch, error) {
	defer func(start time.Time) {
		prometheusmetrics.Observe(q.metrics.requestAssignmentLatencySeconds, time.Since(start))
	}(time.Now())
	batcher, err := q.getBatcher(ctx, query.View)
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
	prometheusmetrics.Observe(q.metrics.requestsLatency.WithLabelValues(method), time.Since(start))
}
