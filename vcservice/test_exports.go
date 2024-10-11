package vcservice

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

// DatabaseTestEnv represents a database test environment.
type DatabaseTestEnv struct {
	DB     *database
	DBConf *DatabaseConfig
}

// NewDatabaseTestEnv creates a new database test environment.
func NewDatabaseTestEnv(t *testing.T) *DatabaseTestEnv {
	cs := yuga.PrepareYugaTestEnv(t)
	port, err := strconv.Atoi(cs.Port)
	require.NoError(t, err)

	config := &DatabaseConfig{
		Host:                  cs.Host,
		Port:                  port,
		Username:              cs.User,
		Password:              cs.Password,
		Database:              cs.Database,
		MaxConnections:        10,
		MinConnections:        1,
		ConnPoolCreateTimeout: 15 * time.Second,
	}

	metrics := newVCServiceMetrics()
	db, err := newDatabase(config, metrics)
	require.NoError(t, err)

	t.Cleanup(func() {
		db.close()
	})

	return &DatabaseTestEnv{
		DB:     db,
		DBConf: config,
	}
}

// CountStatus returns the number of transactions with a given tx status.
func (env *DatabaseTestEnv) CountStatus(t *testing.T, status protoblocktx.Status) int {
	query := fmt.Sprintf("SELECT count(*) FROM tx_status WHERE status = %d", status)
	row := env.DB.pool.QueryRow(context.Background(), query)
	return readCount(t, row)
}

// CountAlternateStatus returns the number of transactions not with a given tx status.
func (env *DatabaseTestEnv) CountAlternateStatus(t *testing.T, status protoblocktx.Status) int {
	query := fmt.Sprintf("SELECT count(*) FROM tx_status WHERE status != %d", status)
	row := env.DB.pool.QueryRow(context.Background(), query)
	return readCount(t, row)
}

func readCount(t *testing.T, r pgx.Row) int {
	var count int
	require.NoError(t, r.Scan(&count))
	return count
}
