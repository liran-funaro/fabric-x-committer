/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package vc

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/protovcservice"
	"github.com/hyperledger/fabric-x-committer/api/types"
	"github.com/hyperledger/fabric-x-committer/service/vc/dbtest"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring"
	"github.com/hyperledger/fabric-x-committer/utils/test"
)

const (
	queryKeyValueVersionSQLTmpt = "SELECT key, value, version FROM %s WHERE key = ANY($1)"
)

type (
	// ValidatorAndCommitterServiceTestEnv denotes the test environment for vcservice.
	ValidatorAndCommitterServiceTestEnv struct {
		VCServices []*ValidatorCommitterService
		DBEnv      *DatabaseTestEnv
		Configs    []*Config
		Endpoints  []*connection.Endpoint
	}

	// ValueVersion contains a list of values and their matching versions.
	ValueVersion struct {
		Value   []byte
		Version uint64
	}
)

// NewValidatorAndCommitServiceTestEnv creates a new test environment with a vcservice and a database.
func NewValidatorAndCommitServiceTestEnv(
	t *testing.T,
	numServices int,
	db ...*DatabaseTestEnv,
) *ValidatorAndCommitterServiceTestEnv {
	t.Helper()
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

	initCtx, initCancel := context.WithTimeout(t.Context(), 2*time.Minute)
	t.Cleanup(initCancel)
	vcservices := make([]*ValidatorCommitterService, numServices)
	configs := make([]*Config, numServices)

	endpoints := make([]*connection.Endpoint, numServices)
	for i := range vcservices {
		config := &Config{
			Server:   connection.NewLocalHostServer(),
			Database: dbEnv.DBConf,
			ResourceLimits: &ResourceLimitsConfig{
				MaxWorkersForPreparer:             2,
				MaxWorkersForValidator:            2,
				MaxWorkersForCommitter:            2,
				MinTransactionBatchSize:           1,
				TimeoutForMinTransactionBatchSize: 20 * time.Second, // to avoid flakyness in TestWaitingTxsCount,
				// we are setting the timeout value to 20 seconds
			},
			Monitoring: monitoring.Config{
				Server: connection.NewLocalHostServer(),
			},
		}
		vcs, err := NewValidatorCommitterService(initCtx, config)
		require.NoError(t, err)
		t.Cleanup(vcs.Close)
		test.RunServiceAndGrpcForTest(t.Context(), t, vcs, config.Server, func(server *grpc.Server) {
			protovcservice.RegisterValidationAndCommitServiceServer(server, vcs)
		})
		vcservices[i] = vcs
		configs[i] = config
		endpoints[i] = &config.Server.Endpoint
	}

	return &ValidatorAndCommitterServiceTestEnv{
		VCServices: vcservices,
		DBEnv:      dbEnv,
		Configs:    configs,
		Endpoints:  endpoints,
	}
}

// GetDBEnv returns the database test environment.
func (vcEnv *ValidatorAndCommitterServiceTestEnv) GetDBEnv() *DatabaseTestEnv {
	if vcEnv == nil {
		return nil
	}
	return vcEnv.DBEnv
}

// SetupSystemTablesAndNamespaces creates the required system tables and namespaces.
func (vcEnv *ValidatorAndCommitterServiceTestEnv) SetupSystemTablesAndNamespaces(ctx context.Context, t *testing.T) {
	t.Helper()
	require.NoError(t, vcEnv.DBEnv.DB.setupSystemTablesAndNamespaces(ctx))
}

// DatabaseTestEnv represents a database test environment.
type DatabaseTestEnv struct {
	DB     *database
	DBConf *DatabaseConfig
}

// NewDatabaseTestEnv creates a new default database test environment.
func NewDatabaseTestEnv(t *testing.T) *DatabaseTestEnv {
	t.Helper()
	// default parameters set.
	return newDatabaseTestEnv(t, dbtest.PrepareTestEnv(t), false)
}

// NewDatabaseTestEnvWithCluster creates a new db cluster test environment.
func NewDatabaseTestEnvWithCluster(t *testing.T, dbConnections *dbtest.Connection) *DatabaseTestEnv {
	t.Helper()
	require.NotNil(t, dbConnections)
	return newDatabaseTestEnv(t, dbtest.PrepareTestEnvWithConnection(t, dbConnections), dbConnections.LoadBalance)
}

func newDatabaseTestEnv(t *testing.T, cs *dbtest.Connection, loadBalance bool) *DatabaseTestEnv {
	t.Helper()
	config := &DatabaseConfig{
		Endpoints:      cs.Endpoints,
		Username:       cs.User,
		Password:       cs.Password,
		Database:       cs.Database,
		MaxConnections: 10,
		MinConnections: 1,
		LoadBalance:    loadBalance,
		Retry: &connection.RetryProfile{
			MaxElapsedTime:  5 * time.Minute,
			InitialInterval: time.Duration(rand.Intn(900)+100) * time.Millisecond,
		},
	}

	m := newVCServiceMetrics()
	sCtx, sCancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(sCancel)
	dbObject, err := newDatabase(sCtx, config, m)
	require.NoError(t, err, "%+v", err)
	t.Cleanup(dbObject.close)

	return &DatabaseTestEnv{
		DB:     dbObject,
		DBConf: config,
	}
}

// CountStatus returns the number of transactions with a given tx status.
func (env *DatabaseTestEnv) CountStatus(t *testing.T, status protoblocktx.Status) int {
	t.Helper()
	return env.getRowCount(t, fmt.Sprintf("SELECT count(*) FROM tx_status WHERE status = %d", status))
}

// CountAlternateStatus returns the number of transactions not with a given tx status.
func (env *DatabaseTestEnv) CountAlternateStatus(t *testing.T, status protoblocktx.Status) int {
	t.Helper()
	return env.getRowCount(t, fmt.Sprintf("SELECT count(*) FROM tx_status WHERE status != %d", status))
}

// queryRow execute a single-row query and return the result.
func (env *DatabaseTestEnv) getRowCount(t *testing.T, query string) int {
	t.Helper()
	var count int
	require.NoError(t, env.DB.retry.Execute(t.Context(), func() error {
		row := env.DB.pool.QueryRow(t.Context(), query)
		return row.Scan(&count)
	}))

	return count
}

// StatusExistsForNonDuplicateTxID ensures that the given statuses and height
// exist for the corresponding txIDs in the tx_status table, excluding any
// duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsForNonDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]*protoblocktx.StatusWithHeight,
) {
	t.Helper()
	var dbStatusTXIDs [][]byte
	for id, s := range expectedStatuses {
		if s.Code != protoblocktx.Status_REJECTED_DUPLICATE_TX_ID {
			dbStatusTXIDs = append(dbStatusTXIDs, []byte(id))
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	actualRows, err := env.DB.readStatusWithHeight(ctx, dbStatusTXIDs)
	require.NoError(t, err)

	require.Len(t, actualRows, len(dbStatusTXIDs))
	for _, tID := range dbStatusTXIDs {
		require.EqualExportedValues(t, expectedStatuses[string(tID)], actualRows[string(tID)])
	}
}

// StatusExistsWithDifferentHeightForDuplicateTxID ensures that the given
// statuses and height do not exist for corresponding txIDs in the tx_status
// table for duplicate txID statuses.
func (env *DatabaseTestEnv) StatusExistsWithDifferentHeightForDuplicateTxID(
	t *testing.T,
	expectedStatuses map[string]*protoblocktx.StatusWithHeight,
) {
	t.Helper()
	var dupTxIDs [][]byte
	for id, s := range expectedStatuses {
		if s.Code == protoblocktx.Status_REJECTED_DUPLICATE_TX_ID {
			dupTxIDs = append(dupTxIDs, []byte(id))
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	actualRows, err := env.DB.readStatusWithHeight(ctx, dupTxIDs)
	require.NoError(t, err)

	require.Len(t, actualRows, len(dupTxIDs))
	for _, tID := range dupTxIDs {
		// For the duplicate txID, neither the status nor the height would match the entry in the
		// transaction status table.
		txID := string(tID) //nolint:staticcheck // false positive.
		require.NotEqual(t, expectedStatuses[txID].Code, actualRows[txID].Code)
		expHeight := types.NewHeight(expectedStatuses[txID].BlockNumber, expectedStatuses[txID].TxNumber)
		actualHeight := types.NewHeight(actualRows[txID].BlockNumber, actualRows[txID].TxNumber)
		require.NotEqual(t, expHeight, actualHeight)
	}
}

func (env *DatabaseTestEnv) populateData( //nolint:revive
	t *testing.T,
	createNsIDs []string,
	nsToWrites namespaceToWrites,
	batchStatus *protoblocktx.TransactionsStatus,
	txIDToHeight transactionIDToHeight,
) {
	t.Helper()
	newNsIDsWrites := namespaceToWrites{}
	for _, nsID := range createNsIDs {
		nsWrites := newNsIDsWrites.getOrCreate(types.MetaNamespaceID)
		nsWrites.append([]byte(nsID), nil, 0)
	}

	require.NoError(t, env.DB.retry.Execute(t.Context(), func() error {
		conflicts, duplicate, err := env.DB.commit(t.Context(), &statesToBeCommitted{
			newWrites: newNsIDsWrites, batchStatus: batchStatus, txIDToHeight: txIDToHeight,
		})
		require.Empty(t, conflicts)
		require.Empty(t, duplicate)
		logger.WarnStackTrace(err)
		return err
	}))

	for nsID, writes := range nsToWrites {
		if writes.empty() {
			continue
		}

		require.NotNil(t, writes.keys)
		require.NotNil(t, writes.values)
		require.NotNil(t, writes.versions)
		require.Len(t, writes.values, len(writes.keys))
		require.Len(t, writes.versions, len(writes.keys))

		insertQuery := `
INSERT INTO ns_${NAMESPACE_ID} (key, value, version)
SELECT _key, _value, _version
FROM UNNEST($1::bytea[], $2::bytea[], $3::bigint[]) AS t(_key, _value, _version);
`
		query := FmtNsID(insertQuery, nsID)
		require.NoError(t, env.DB.retry.ExecuteSQL(t.Context(), env.DB.pool, query,
			writes.keys, writes.values, writes.versions,
		))
	}
}

// FetchKeys fetches a list of keys.
func (env *DatabaseTestEnv) FetchKeys(t *testing.T, nsID string, keys [][]byte) map[string]*ValueVersion {
	t.Helper()
	query := fmt.Sprintf(queryKeyValueVersionSQLTmpt, TableName(nsID))

	kvPairs, err := env.DB.pool.Query(t.Context(), query, keys)
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
	t.Helper()
	query := fmt.Sprintf(
		"SELECT table_name FROM information_schema.tables WHERE table_name = '%s'", TableName(nsID),
	)
	names, err := env.DB.pool.Query(t.Context(), query)
	require.NoError(t, err)
	defer names.Close()
	require.True(t, names.Next())
}

func (env *DatabaseTestEnv) rowExists(t *testing.T, nsID string, exp namespaceWrites) {
	t.Helper()
	actualRows := env.FetchKeys(t, nsID, exp.keys)

	assert.Len(t, actualRows, len(exp.keys))
	for i, key := range exp.keys {
		if assert.NotNil(t, actualRows[string(key)], "key: %s", string(key)) {
			assert.Equal(t, exp.values[i], actualRows[string(key)].Value, "key: %s", string(key))
			assert.EqualExportedValuesf(t, exp.versions[i], actualRows[string(key)].Version, "key: %s", string(key))
		}
	}
}

func (env *DatabaseTestEnv) rowNotExists(t *testing.T, nsID string, keys [][]byte) {
	t.Helper()
	actualRows := env.FetchKeys(t, nsID, keys)
	assert.Empty(t, actualRows)
	for key, valVer := range actualRows {
		assert.Failf(t, "Key should not exist", "key [%s] value: [%s] version [%d]",
			key, string(valVer.Value), valVer.Version)
	}
}
