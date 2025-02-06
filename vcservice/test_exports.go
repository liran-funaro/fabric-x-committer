package vcservice

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

const (
	queryKeyValueVersionSQLTmpt = "SELECT key, value, version FROM %s WHERE key = ANY($1)"
)

// ValidatorAndCommitterServiceTestEnv denotes the test environment for vcservice.
type ValidatorAndCommitterServiceTestEnv struct {
	VCService *ValidatorCommitterService
	DBEnv     *DatabaseTestEnv
	Config    *ValidatorCommitterServiceConfig
}

// ValueVersion contains a list of values and their matching versions.
type ValueVersion struct {
	Value   []byte
	Version []byte
}

// NewValidatorAndCommitServiceTestEnv creates a new test environment with a vcservice and a database.
func NewValidatorAndCommitServiceTestEnv(
	t *testing.T,
	db ...*DatabaseTestEnv,
) *ValidatorAndCommitterServiceTestEnv {
	require.LessOrEqual(t, len(db), 1)

	var dbEnv *DatabaseTestEnv
	switch len(db) {
	case 0:
		dbEnv = NewDatabaseTestEnv(t)
	case 1:
		dbEnv = db[0]
	default:
		t.Fatalf("At most one db env can be passed as n argument but received %d envs", len(db))
	}

	config := &ValidatorCommitterServiceConfig{
		Server: &connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: 0,
			},
		},
		Database: dbEnv.DBConf,
		ResourceLimits: &ResourceLimitsConfig{
			MaxWorkersForPreparer:             2,
			MaxWorkersForValidator:            2,
			MaxWorkersForCommitter:            2,
			MinTransactionBatchSize:           1,
			TimeoutForMinTransactionBatchSize: 20 * time.Second, // to avoid flakyness in TestWaitingTxsCount, we are
			// setting the timeout value to 20 seconds
		},
		Monitoring: &monitoring.Config{
			Metrics: &metrics.Config{
				Enable: true,
				Endpoint: &connection.Endpoint{
					Host: "localhost",
					Port: 0,
				},
			},
		},
	}

	initCtx, initCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(initCancel)
	vcs, err := NewValidatorCommitterService(initCtx, config)
	require.NoError(t, err)
	t.Cleanup(vcs.Close)
	test.RunServiceAndGrpcForTest(context.Background(), t, vcs, config.Server, func(server *grpc.Server) {
		protovcservice.RegisterValidationAndCommitServiceServer(server, vcs)
	})

	return &ValidatorAndCommitterServiceTestEnv{
		VCService: vcs,
		DBEnv:     dbEnv,
		Config:    config,
	}
}

// GetDBEnv returns the database test environment.
func (vcEnv *ValidatorAndCommitterServiceTestEnv) GetDBEnv(_ *testing.T) *DatabaseTestEnv {
	if vcEnv == nil {
		return nil
	}
	return vcEnv.DBEnv
}

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
		Host:           cs.Host,
		Port:           port,
		Username:       cs.User,
		Password:       cs.Password,
		Database:       cs.Database,
		MaxConnections: 10,
		MinConnections: 1,
	}

	m := newVCServiceMetrics()
	sCtx, sCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(sCancel)
	db, err := newDatabase(sCtx, config, m)
	require.NoError(t, err)
	t.Cleanup(db.close)

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
	expectedStatuses map[string]*protoblocktx.StatusWithHeight,
) {
	var nonDupTxIDs [][]byte
	for id, s := range expectedStatuses {
		if s.Code != protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			nonDupTxIDs = append(nonDupTxIDs, []byte(id))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	actualRows, err := env.DB.readStatusWithHeight(ctx, nonDupTxIDs)
	require.NoError(t, err)

	require.Equal(t, len(nonDupTxIDs), len(actualRows))
	for _, tID := range nonDupTxIDs {
		require.Equal(t, expectedStatuses[string(tID)], actualRows[string(tID)])
	}
}

// StatusExistsWithDifferentHeightForDuplicateTxID ensures that the given
// statuses and height do not exist for corresponding txIDs in the tx_status
// table for duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsWithDifferentHeightForDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]*protoblocktx.StatusWithHeight,
) {
	var dupTxIDs [][]byte
	for id, s := range expectedStatuses {
		if s.Code == protoblocktx.Status_ABORTED_DUPLICATE_TXID {
			dupTxIDs = append(dupTxIDs, []byte(id))
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	actualRows, err := env.DB.readStatusWithHeight(ctx, dupTxIDs)
	require.NoError(t, err)

	require.Equal(t, len(dupTxIDs), len(actualRows))
	for _, tID := range dupTxIDs {
		// For the duplicate txID, neither the status nor the height would match the entry in the
		// transaction status table.
		txID := string(tID) // nolint:staticcheck
		require.NotEqual(t, expectedStatuses[txID].Code, actualRows[txID].Code)
		expHeight := types.NewHeight(expectedStatuses[txID].BlockNumber, expectedStatuses[txID].TxNumber)
		actualHeight := types.NewHeight(actualRows[txID].BlockNumber, actualRows[txID].TxNumber)
		require.NotEqual(t, expHeight, actualHeight)
	}
}

func (env *DatabaseTestEnv) commitState(t *testing.T, nsToWrites namespaceToWrites) {
	for nsID, writes := range nsToWrites {
		_, err := env.DB.pool.Exec(context.Background(), fmt.Sprintf(`
			INSERT INTO %s (key, value, version)
			SELECT _key, _value, _version
			FROM UNNEST($1::bytea[], $2::bytea[], $3::bytea[]) AS t(_key, _value, _version);`,
			TableName(nsID)),
			writes.keys, writes.values, writes.versions,
		)
		require.NoError(t, err)
	}
}

func (env *DatabaseTestEnv) populateDataWithCleanup( //nolint:revive
	t *testing.T,
	nsIDs []string,
	writes namespaceToWrites,
	batchStatus *protoblocktx.TransactionsStatus,
	txIDToHeight transactionIDToHeight,
) {
	require.NoError(t, initDatabaseTables(context.Background(), env.DB.pool, nsIDs))

	_, _, err := env.DB.commit(&statesToBeCommitted{batchStatus: batchStatus, txIDToHeight: txIDToHeight})
	require.NoError(t, err)
	env.commitState(t, writes)

	t.Cleanup(func() {
		require.NoError(t, clearDatabaseTables(context.Background(), env.DB.pool, nsIDs))
	})
}

// FetchKeys fetches a list of keys.
func (env *DatabaseTestEnv) FetchKeys(t *testing.T, nsID string, keys [][]byte) map[string]*ValueVersion {
	query := fmt.Sprintf(queryKeyValueVersionSQLTmpt, TableName(nsID))

	kvPairs, err := env.DB.pool.Query(context.Background(), query, keys)
	require.NoError(t, err)
	defer kvPairs.Close()

	actualRows := map[string]*ValueVersion{}

	for kvPairs.Next() {
		var key []byte
		vv := &ValueVersion{}

		require.NoError(t, kvPairs.Scan(&key, &vv.Value, &vv.Version))
		actualRows[string(key)] = vv
	}

	require.NoError(t, kvPairs.Err())

	return actualRows
}

func (env *DatabaseTestEnv) tableExists(t *testing.T, nsID string) {
	query := fmt.Sprintf("SELECT table_name FROM information_schema.tables WHERE table_name = '%s'", TableName(nsID))
	names, err := env.DB.pool.Query(context.Background(), query)
	require.NoError(t, err)
	defer names.Close()
	require.True(t, names.Next())
}

func (env *DatabaseTestEnv) rowExists(t *testing.T, nsID string, expectedRows namespaceWrites) {
	actualRows := env.FetchKeys(t, nsID, expectedRows.keys)

	assert.Len(t, actualRows, len(expectedRows.keys))
	for i, key := range expectedRows.keys {
		if assert.NotNil(t, actualRows[string(key)], "key: %s", string(key)) {
			assert.Equal(t, expectedRows.values[i], actualRows[string(key)].Value, "key: %s", string(key))
			assert.Equal(t, expectedRows.versions[i], actualRows[string(key)].Version, "key: %s", string(key))
		}
	}
}

func (env *DatabaseTestEnv) rowNotExists(t *testing.T, nsID string, keys [][]byte) {
	actualRows := env.FetchKeys(t, nsID, keys)

	assert.Len(t, actualRows, 0)
	for key, valVer := range actualRows {
		assert.Fail(t, "key [%s] should not exist; value: [%s], version [%d]",
			key, string(valVer.Value), types.VersionNumberFromBytes(valVer.Version))
	}
}
