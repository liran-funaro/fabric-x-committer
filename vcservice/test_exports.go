package vcservice

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
