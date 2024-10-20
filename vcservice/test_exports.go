package vcservice

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yugabyte/pgx/v4"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

const queryTxStatusSQLTemplate = "SELECT tx_id, status, height FROM tx_status WHERE tx_id = ANY($1)"

// ValidatorAndCommitterServiceTestEnv denotes the test environment for vcservice.
type ValidatorAndCommitterServiceTestEnv struct {
	VCService *ValidatorCommitterService
	DBEnv     *DatabaseTestEnv
	Config    *ValidatorCommitterServiceConfig
}

// NewValidatorAndCommitServiceTestEnv creates a new test environment with a vcservice and a database.
func NewValidatorAndCommitServiceTestEnv(
	ctx context.Context,
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

	vcs, err := NewValidatorCommitterService(config)
	require.NoError(t, err)
	t.Cleanup(vcs.Close)

	wg := sync.WaitGroup{}
	t.Cleanup(wg.Wait)

	sCtx, sCancel := context.WithCancel(ctx)
	t.Cleanup(sCancel)
	wg.Add(1)
	go func() { require.NoError(t, vcs.Run(sCtx)); wg.Done() }()

	var grpcSrv *grpc.Server
	var serviceG sync.WaitGroup
	serviceG.Add(1)

	go func() {
		connection.RunServerMain(config.Server, func(grpcServer *grpc.Server, actualListeningPort int) {
			grpcSrv = grpcServer
			config.Server.Endpoint.Port = actualListeningPort
			protovcservice.RegisterValidationAndCommitServiceServer(grpcServer, vcs)
			serviceG.Done()
		})
	}()

	serviceG.Wait()
	stop := context.AfterFunc(ctx, grpcSrv.Stop)
	t.Cleanup(func() { stop() })

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
		Host:                  cs.Host,
		Port:                  port,
		Username:              cs.User,
		Password:              cs.Password,
		Database:              cs.Database,
		MaxConnections:        10,
		MinConnections:        1,
		ConnPoolCreateTimeout: 15 * time.Second,
	}

	m := newVCServiceMetrics()
	db, err := newDatabase(config, m)
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
