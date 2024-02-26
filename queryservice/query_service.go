package queryservice

import (
	"context"
	"fmt"
	"time"

	"github.com/yugabyte/pgx/v4/pgxpool"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

const (
	// queryRowSQLTemplate template for the querying rows for each namespace.
	queryRowSQLTemplate = "SELECT key, value, version FROM ns_%d WHERE key = ANY($1);"
)

var logger = logging.New("query service")

type (
	// QueryService is a gRPC service that implements the QueryServiceServer interface.
	QueryService struct {
		protoqueryservice.UnimplementedQueryServiceServer
		pool        *pgxpool.Pool
		metrics     *perfMetrics
		metricsHost *connection.Endpoint
	}
)

// NewQueryService create a new QueryService given a configuration.
func NewQueryService(config *Config) (*QueryService, error) {
	metrics := newQueryServiceMetrics()
	pool, err := vcservice.NewDatabasePool(config.Database)
	if err != nil {
		return nil, err
	}
	return &QueryService{
		pool:        pool,
		metrics:     metrics,
		metricsHost: config.Monitoring.Metrics.Endpoint,
	}, nil
}

// Close releases the QueryService resources.
func (q *QueryService) Close() {
	q.pool.Close()
	if err := q.metrics.StopServer(); err != nil {
		logger.Errorf("Failed stopping prometheus server: %s", err)
	}
}

// StartPrometheusServer see Provider.StartPrometheusServer().
func (q *QueryService) StartPrometheusServer() <-chan error {
	return q.metrics.StartPrometheusServer(q.metricsHost)
}

// GetRows implements the query-service interface.
func (q *QueryService) GetRows(
	ctx context.Context, ds *protoqueryservice.Query,
) (*protoqueryservice.Rows, error) {
	prometheusmetrics.AddToCounter(q.metrics.queriesReceivedTotal, 1)

	res := &protoqueryservice.Rows{
		Namespaces: make([]*protoqueryservice.RowsNamespace, len(ds.Namespaces)),
	}

	start := time.Now()
	for i, ns := range ds.Namespaces {
		prometheusmetrics.AddToCounter(q.metrics.keyQueriedTotal, len(ns.Keys))
		var err error
		res.Namespaces[i], err = q.queryRowsIfPresent(ctx, ns)
		if err != nil {
			return nil, err
		}
	}
	prometheusmetrics.Observe(q.metrics.queryLatencySeconds, time.Since(start))

	return res, nil
}

// queryRowsIfPresent queries rows for the given keys if they exist.
func (q *QueryService) queryRowsIfPresent(
	ctx context.Context, query *protoqueryservice.QueryNamespace,
) (*protoqueryservice.RowsNamespace, error) {
	queryStmt := fmt.Sprintf(queryRowSQLTemplate, query.NsId)
	r, err := q.pool.Query(ctx, queryStmt, query.Keys)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	res := &protoqueryservice.RowsNamespace{
		NsId: query.NsId,
		Rows: make([]*protoqueryservice.Row, 0, len(query.Keys)),
	}

	for r.Next() {
		v := &protoqueryservice.Row{}
		if err = r.Scan(&v.Key, &v.Value, &v.Version); err != nil {
			return nil, err
		}
		res.Rows = append(res.Rows, v)
	}

	return res, r.Err()
}
