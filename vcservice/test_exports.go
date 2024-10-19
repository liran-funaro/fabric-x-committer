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

// StatusExistsForNonDuplicateTxID ensures that the given statuses and height
// exist for the corresponding txIDs in the tx_status table, excluding any
// duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsForNonDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]protoblocktx.Status,
	expectedHeight transactionIDToHeight,
) {
	var nonDupTxIDs [][]byte
	for id, s := range expectedStatuses {
		if s != protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			nonDupTxIDs = append(nonDupTxIDs, []byte(id))
		}
	}

	actualRows, err := env.DB.readStatusWithHeight(nonDupTxIDs)
	require.NoError(t, err)

	require.Equal(t, len(nonDupTxIDs), len(actualRows))
	for _, tID := range nonDupTxIDs {
		txID := TxID(tID)
		require.Equal(t, expectedStatuses[string(txID)], actualRows[txID].status)
		require.Equal(t, expectedHeight[txID], actualRows[txID].height)
	}
}

// StatusExistsWithDifferentHeightForDuplicateTxID ensures that the given
// statuses and height do not exist for corresponding txIDs in the tx_status
// table for duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsWithDifferentHeightForDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]protoblocktx.Status,
	unexpectedHeight transactionIDToHeight,
) {
	var dupTxIDs [][]byte
	for id, s := range expectedStatuses {
		if s == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			dupTxIDs = append(dupTxIDs, []byte(id))
		}
	}
	actualRows, err := env.DB.readStatusWithHeight(dupTxIDs)
	require.NoError(t, err)

	require.Equal(t, len(dupTxIDs), len(actualRows))
	for _, tID := range dupTxIDs {
		// For the duplicate txID, neither the status nor the height would match the entry in the
		// transaction status table.
		txID := TxID(tID)
		require.NotEqual(t, expectedStatuses[string(txID)], actualRows[txID].status)
		require.NotEqual(t, unexpectedHeight[txID], actualRows[txID].height)
	}
}
