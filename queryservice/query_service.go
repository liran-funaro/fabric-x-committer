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

// GetCommitments implements the query-service interface.
func (q *QueryService) GetCommitments(
	ctx context.Context, ds *protoqueryservice.TokenIDs,
) (*protoqueryservice.Commitments, error) {
	prometheusmetrics.AddToCounter(q.metrics.queriesReceivedTotal, 1)
	prometheusmetrics.AddToCounter(q.metrics.keyQueriedTotal, len(ds.Keys))

	start := time.Now()
	rows, err := q.queryRowsIfPresent(ctx, 0, ds.Keys)
	if err != nil {
		return nil, err
	}
	prometheusmetrics.Observe(q.metrics.queryLatencySeconds, time.Since(start))

	output := make([]*protoqueryservice.ValueWithVersion, len(ds.Keys))

	for i, k := range ds.Keys {
		if v, ok := rows[string(k)]; ok {
			output[i] = v
		}
	}

	return &protoqueryservice.Commitments{
		Output: output,
	}, nil
}

// queryRowsIfPresent queries rows for the given keys if they exist.
func (q *QueryService) queryRowsIfPresent(
	ctx context.Context, nsID vcservice.NamespaceID, queryKeys [][]byte,
) (map[string]*protoqueryservice.ValueWithVersion, error) {
	query := fmt.Sprintf(queryRowSQLTemplate, nsID)
	r, err := q.pool.Query(ctx, query, queryKeys)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	res := make(map[string]*protoqueryservice.ValueWithVersion)

	for r.Next() {
		var key []byte
		v := protoqueryservice.ValueWithVersion{}
		if err = r.Scan(&key, &v.Value, &v.Version); err != nil {
			return nil, err
		}
		res[string(key)] = &v
	}

	return res, r.Err()
}
