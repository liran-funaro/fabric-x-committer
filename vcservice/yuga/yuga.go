package yuga

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultStartTimeout   = 5 * time.Minute
	defaultDBPrefix       = "yuga_db_unit_tests_"
	yugaInstanceLocal     = "local"
	yugaInstanceContainer = "container"
	yugaDBPort            = "5433"
)

// DBOptions defines the YugaDB cluster initialization configuration.
type DBOptions struct {
	ClusterSize int
	Connections []*Connection
}

// randDbName generates random DB name.
func randDbName(t *testing.T) string {
	t.Helper()
	uuidObj, err := uuid.NewRandomFromReader(rand.Reader)
	require.NoError(t, err)
	uuidStr := strings.ReplaceAll(uuidObj.String(), "-", "_")
	return fmt.Sprintf("%s%s", defaultDBPrefix, uuidStr)
}

// getYugaInstanceType get the desired yuga instance type from the environment variable.
func getYugaInstanceType() string {
	val, found := os.LookupEnv("DB_INSTANCE")
	if found {
		return strings.ToLower(val)
	}

	return yugaInstanceContainer
}

// PrepareTestEnv initializes a test environment for an existing or uncontrollable db instance.
func PrepareTestEnv(t *testing.T) *Connection {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), defaultStartTimeout)
	t.Cleanup(cancel)
	return PrepareTestEnvWithConnection(t, StartAndConnect(ctx, t))
}

// PrepareTestEnvWithConnection initializes a test environment given a db connection.
func PrepareTestEnvWithConnection(t *testing.T, connections []*Connection) *Connection {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), defaultStartTimeout)
	t.Cleanup(cancel)
	conn, err := WaitFirstReady(ctx, connections)
	require.NoError(t, err)
	t.Logf("chosen connection details: %s:%s", conn.Host, conn.Port)

	dbName := randDbName(t)
	require.NoError(t, conn.CreateDB(ctx, dbName))

	t.Cleanup(func() {
		//nolint:usetesting // t.Context is finishing right after the test resulting in context.Deadline error.
		cleanUpCtx, cleanUpCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cleanUpCancel()
		assert.NoError(t, DefaultRetry.Execute(cleanUpCtx, func() error {
			return conn.DropDB(cleanUpCtx, dbName)
		}))
	})
	// We copy the connection and add the database name
	connSettings := *conn
	connSettings.Database = dbName

	return &connSettings
}

// StartAndConnect connects to an existing Yugabyte instance or creates a containerized new one.
func StartAndConnect(ctx context.Context, t *testing.T) []*Connection {
	t.Helper()
	yugaInstance := getYugaInstanceType()

	var connOptions []*Connection
	switch yugaInstance {
	case yugaInstanceContainer:
		container := YugabyteDBContainer{}
		container.StartContainer(ctx, t)
		connOptions = container.getConnectionOptions(ctx, t)
	case yugaInstanceLocal:
		connOptions = append(connOptions, NewConnection("localhost", yugaDBPort))
	default:
		t.Logf("unknown yuga instance type: %s", yugaInstance)
		return nil
	}
	return connOptions
}
