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
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

const queryTxStatusSQLTemplate = "SELECT tx_id, status, height FROM tx_status WHERE tx_id = ANY($1)"

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

type statusWithHeight struct {
	status int
	height []byte
}

// StatusExistsForNonDuplicateTxID ensures that the given statuses and height
// exist for the corresponding txIDs in the tx_status table, excluding any
// duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsForNonDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]protoblocktx.Status,
	expectedHeight map[TxID]*types.Height,
) {
	actualRows := env.queryStatus(t, expectedStatuses)

	require.Equal(t, len(expectedStatuses), len(actualRows))
	for tID, status := range expectedStatuses {
		// "duplicated TX ID" status is never committed.
		if status == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			continue
		}
		require.Equal(t, int(status), actualRows[TxID(tID)].status)
		require.Equal(t, expectedHeight[TxID(tID)].ToBytes(), actualRows[TxID(tID)].height)
	}
}

// StatusExistsWithDifferentHeightForDuplicateTxID ensures that the given
// statuses and height do not exist for corresponding txIDs in the tx_status
// table for duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsWithDifferentHeightForDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]protoblocktx.Status,
	unexpectedHeight map[TxID]*types.Height,
) {
	actualRows := env.queryStatus(t, expectedStatuses)

	require.Equal(t, len(expectedStatuses), len(actualRows))
	for tID, status := range expectedStatuses {
		// For the duplicate txID, neither the status nor the height would match the entry in the
		// transaction status table.
		require.NotEqual(t, int(status), actualRows[TxID(tID)].status)
		require.NotEqual(t, unexpectedHeight[TxID(tID)].ToBytes(), actualRows[TxID(tID)].height)
	}
}

func (env *DatabaseTestEnv) queryStatus(
	t *testing.T,
	expectedStatuses map[string]protoblocktx.Status,
) map[TxID]*statusWithHeight {
	expectedIDs := make([][]byte, 0, len(expectedStatuses))
	for id := range expectedStatuses {
		expectedIDs = append(expectedIDs, []byte(id))
	}
	idStatusHeight, err := env.DB.pool.Query(context.Background(), queryTxStatusSQLTemplate, expectedIDs)
	require.NoError(t, err)
	defer idStatusHeight.Close()

	actualRows := map[TxID]*statusWithHeight{}

	for idStatusHeight.Next() {
		var key []byte
		var status int
		var height []byte
		require.NoError(t, idStatusHeight.Scan(&key, &status, &height))
		actualRows[TxID(key)] = &statusWithHeight{status: status, height: height}
	}
	require.NoError(t, idStatusHeight.Err())

	return actualRows
}
