package vcservice

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_DbInit(t *testing.T) {
	env := newDatabaseTestEnv(t)

	ns := []int{0, 1, 2, 3}
	require.NoError(t, initDatabaseTables(env.db, ns))

	_, err := env.db.pool.Exec(context.Background(), `insert into ns_0 values (UNNEST($1::bytea[]));`, [][]byte{
		[]byte("tx1"), []byte("tx2"), []byte("tx3"), []byte("tx4"),
	})
	require.NoError(t, err)

	// Validate default values
	r, err := env.db.pool.Query(context.Background(), `select * from ns_0;`)
	require.NoError(t, err)
	defer r.Close()
	for r.Next() {
		var key, value, version []byte
		require.NoError(t, r.Scan(&key, &value, &version))
		t.Logf("key: %s", string(key))
		require.Nil(t, value)
		require.Equal(t, versionNumber(0).bytes(), version)
	}

	require.NoError(t, clearDatabaseTables(env.db, ns))
}
