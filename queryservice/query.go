package queryservice

import (
	"context"
	_ "embed"
	"fmt"
	"sync"

	"github.com/yugabyte/pgx/v4"
	"github.com/yugabyte/pgx/v4/pgxpool"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
)

//go:embed query.sql
var queryRowSQLTemplate string

// sharedPool and sharedLazyTx implements of sharedQuerier.
// sharedPool returns a new connection (implements acquiredQuerier)
// from the pool for every acquire.
// sharedLazyTx locks the transaction to make sure only one goroutine
// uses the connection, and returns itself (implements acquiredQuerier).
// This is because a transaction's connection cannot be used concurrently.
// It also creates the transaction lazily if it was not created yet.
type (
	sharedPool struct {
		pool *pgxpool.Pool
	}
	sharedLazyTx struct {
		pgx.Tx
		m       sync.Mutex
		ctx     context.Context
		metrics *perfMetrics
		options *pgx.TxOptions
		pool    *pgxpool.Pool
	}
	sharedQuerier interface {
		Acquire(ctx context.Context) (acquiredQuerier, error)
	}
	acquiredQuerier interface {
		querier
		Release()
	}
	querier interface {
		Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	}
)

func (t *sharedPool) Acquire(ctx context.Context) (acquiredQuerier, error) {
	return t.pool.Acquire(ctx)
}

func (t *sharedLazyTx) Acquire(ctx context.Context) (acquiredQuerier, error) {
	t.m.Lock()
	m := t.metrics.processingSessions.WithLabelValues(sessionTransactions)
	if t.Tx == nil {
		dbTx, err := t.pool.BeginTx(ctx, *t.options)
		if err != nil {
			t.m.Unlock()
			return nil, err
		}
		t.Tx = dbTx
		m.Inc()
		context.AfterFunc(t.ctx, func() {
			m.Dec()
			// We use background context to rollback transactions to avoid
			// dangling transactions in the database.
			_ = dbTx.Rollback(context.Background())
		})
	}
	return t, nil
}

func (t *sharedLazyTx) Release() {
	t.m.Unlock()
}

// unsafeQueryRows queries rows for the given keys.
// It may not support concurrent execution, depending on the querier implementation.
func unsafeQueryRows(
	ctx context.Context, queryObj querier, nsID string, keys [][]byte,
) ([]*protoqueryservice.Row, error) {
	queryStmt := fmt.Sprintf(queryRowSQLTemplate, nsID)
	r, err := queryObj.Query(ctx, queryStmt, keys)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	rows := make([]*protoqueryservice.Row, 0, len(keys))
	for r.Next() {
		v := &protoqueryservice.Row{}
		if err = r.Scan(&v.Key, &v.Value, &v.Version); err != nil {
			return nil, err
		}
		rows = append(rows, v)
	}
	return rows, r.Err()
}
