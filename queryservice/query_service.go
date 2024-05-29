package queryservice

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yugabyte/pgx/v4"
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

var (
	logger = logging.New("query service")

	// ErrInvalidViewID is returned when attempting to use stale or wrong view ID.
	ErrInvalidViewID = errors.New("invalid view ID")
)

type (
	// QueryService is a gRPC service that implements the QueryServiceServer interface.
	QueryService struct {
		protoqueryservice.UnimplementedQueryServiceServer
		pool        *pgxpool.Pool
		metrics     *perfMetrics
		metricsHost *connection.Endpoint
		views       sync.Map
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
	ctx context.Context, query *protoqueryservice.Query,
) (*protoqueryservice.Rows, error) {
	prometheusmetrics.AddToCounter(q.metrics.queriesReceivedTotal, 1)

	var queryObj queryable
	if query.View == nil {
		queryObj = q.pool
	} else {
		loadedTx, ok := q.views.Load(query.View.Id)
		if !ok {
			return nil, ErrInvalidViewID
		}
		queryObj, _ = loadedTx.(queryable) // nolint:revive
	}

	res := &protoqueryservice.Rows{
		Namespaces: make([]*protoqueryservice.RowsNamespace, len(query.Namespaces)),
	}
	err := q.queryNamespaces(ctx, queryObj, query.Namespaces, res.Namespaces)
	if err != nil && query.View != nil {
		if _, closeErr := q.EndView(ctx, query.View); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}
	return res, err
}

// BeginView implements the query-service interface.
func (q *QueryService) BeginView(
	ctx context.Context, params *protoqueryservice.ViewParameters,
) (*protoqueryservice.View, error) {
	viewID, err := getUUID()
	if err != nil {
		return nil, err
	}

	tx, err := q.pool.BeginTx(ctx, makeTxOptions(params))
	if err != nil {
		return nil, err
	}
	q.views.Store(viewID, tx)
	return &protoqueryservice.View{Id: viewID}, nil
}

func makeTxOptions(p *protoqueryservice.ViewParameters) pgx.TxOptions {
	o := pgx.TxOptions{
		AccessMode:     pgx.ReadOnly,
		IsoLevel:       pgx.Serializable,
		DeferrableMode: pgx.Deferrable,
	}
	if p == nil {
		return o
	}
	switch p.IsoLevel {
	case protoqueryservice.IsoLevel_Serializable:
		o.IsoLevel = pgx.Serializable
	case protoqueryservice.IsoLevel_RepeatableRead:
		o.IsoLevel = pgx.RepeatableRead
	case protoqueryservice.IsoLevel_ReadCommitted:
		o.IsoLevel = pgx.ReadCommitted
	case protoqueryservice.IsoLevel_ReadUncommitted:
		o.IsoLevel = pgx.ReadUncommitted
	}
	if p.NonDeferrable {
		o.DeferrableMode = pgx.NotDeferrable
	} else {
		o.DeferrableMode = pgx.Deferrable
	}
	return o
}

// EndView implements the query-service interface.
func (q *QueryService) EndView(
	ctx context.Context, view *protoqueryservice.View,
) (*protoqueryservice.View, error) {
	loadedTx, ok := q.views.LoadAndDelete(view.Id)
	if !ok {
		return nil, ErrInvalidViewID
	}
	tx, _ := loadedTx.(pgx.Tx) // nolint:revive
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return nil, err
	}
	return view, nil
}

type queryable interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (q *QueryService) queryNamespaces(
	ctx context.Context, queryObj queryable,
	query []*protoqueryservice.QueryNamespace, response []*protoqueryservice.RowsNamespace,
) error {
	start := time.Now()
	for i, ns := range query {
		prometheusmetrics.AddToCounter(q.metrics.keyQueriedTotal, len(ns.Keys))
		var err error
		response[i], err = queryRowsIfPresent(ctx, queryObj, ns)
		if err != nil {
			return err
		}
	}
	prometheusmetrics.Observe(q.metrics.queryLatencySeconds, time.Since(start))
	return nil
}

// queryRowsIfPresent queries rows for the given keys if they exist.
func queryRowsIfPresent(
	ctx context.Context, queryObj queryable, query *protoqueryservice.QueryNamespace,
) (*protoqueryservice.RowsNamespace, error) {
	queryStmt := fmt.Sprintf(queryRowSQLTemplate, query.NsId)
	r, err := queryObj.Query(ctx, queryStmt, query.Keys)
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

func getUUID() (string, error) {
	uuidObj, err := uuid.NewRandomFromReader(rand.Reader)
	if err != nil {
		return "", err
	}
	return uuidObj.String(), nil
}
