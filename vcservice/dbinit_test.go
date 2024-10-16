package vcservice

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

func Test_DbInit(t *testing.T) {
	env := NewDatabaseTestEnv(t)

	ns := []int{0, 1, 2, 3}
	require.NoError(t, initDatabaseTables(context.Background(), env.DB.pool, ns))

	_, err := env.DB.pool.Exec(context.Background(), `insert into ns_0 values (UNNEST($1::bytea[]));`, [][]byte{
		[]byte("tx1"), []byte("tx2"), []byte("tx3"), []byte("tx4"),
	})
	require.NoError(t, err)

	// Validate default values
	r, err := env.DB.pool.Query(context.Background(), `select * from ns_0;`)
	require.NoError(t, err)
	defer r.Close()
	for r.Next() {
		var key, value, version []byte
		require.NoError(t, r.Scan(&key, &value, &version))
		t.Logf("key: %s", string(key))
		require.Nil(t, value)
		require.Equal(t, types.VersionNumber(0).Bytes(), version)
	}

	require.NoError(t, clearDatabaseTables(context.Background(), env.DB.pool, ns))
}

func TestRetry(t *testing.T) {
	pool, err := NewDatabasePool(&DatabaseConfig{
		Host:                  "",
		Port:                  1234,
		Username:              "name",
		Password:              "pwd",
		MaxConnections:        5,
		ConnPoolCreateTimeout: 15 * time.Second,
	})
	require.ErrorContains(t, err, "failed making pool")
	require.Nil(t, pool)
}

func TestConcurrentDatabaseTablesInit(t *testing.T) {
	cs := yuga.PrepareYugaTestEnv(t)
	port, err := strconv.Atoi(cs.Port)
	require.NoError(t, err)

	config := &DatabaseConfig{
		Host:                  cs.Host,
		Port:                  port,
		Username:              cs.User,
		Password:              cs.Password,
		Database:              cs.Database,
		MaxConnections:        15,
		MinConnections:        1,
		ConnPoolCreateTimeout: 3 * time.Minute,
	}
	var wg sync.WaitGroup

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			metrics := newVCServiceMetrics()
			db, dbInitErr := newDatabase(config, metrics)
			require.NoError(t, dbInitErr)
			t.Cleanup(db.close)
		}()
	}
	wg.Wait()
}
