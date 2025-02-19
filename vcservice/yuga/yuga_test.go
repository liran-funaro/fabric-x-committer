package yuga

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ##########################################
// # Tests
// ##########################################

func Test_StartAndQuery(t *testing.T) {
	connSettings := PrepareTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	conn, err := connSettings.Open(ctx)
	require.NoError(t, err)
	defer conn.Close()
	require.Nil(t, conn.Ping(ctx))

	rows, err := conn.Query(ctx, "select distinct tablename from pg_catalog.pg_tables;")
	require.NoError(t, err)
	defer rows.Close()

	var allTables []string
	for rows.Next() {
		require.NoError(t, rows.Err())
		var tableName string
		require.NoError(t, rows.Scan(&tableName))
		allTables = append(allTables, tableName)
	}
	t.Logf("tables: %s", allTables)
}
