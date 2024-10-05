package vcservice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
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
