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

// randDbName generates random DB name.
func randDbName() (string, error) {
	uuidObj, err := uuid.NewRandomFromReader(rand.Reader)
	if err != nil {
		return "", err
	}
	uuidStr := strings.ReplaceAll(uuidObj.String(), "-", "_")
	return fmt.Sprintf("%s%s", defaultDBPrefix, uuidStr), nil
}

// getYugaInstanceType get the desired yuga instance type from the environment variable.
func getYugaInstanceType() string {
	val, found := os.LookupEnv("YUGA_INSTANCE")
	if found {
		return strings.ToLower(val)
	}

	return yugaInstanceContainer
}

// PrepareYugaTestEnv initializes a test environment for YugabyteDB.
func PrepareYugaTestEnv(t *testing.T) *Connection {
	// Impose 5 minutes timeout for starting/connecting to the container.
	ctx, cancel := context.WithTimeout(context.Background(), defaultStartTimeout)
	defer cancel()

	conn, err := StartAndConnect(ctx)
	require.NoError(t, err)

	dbName, err := randDbName()
	require.NoError(t, err)

	t.Cleanup(func() {
		assert.NoError(t, conn.DropDB(context.Background(), dbName))
	})
	require.NoError(t, conn.CreateDB(ctx, dbName))

	// We copy the connection and add the database name.
	connSettings := *conn
	connSettings.Database = dbName
	return &connSettings
}

// StartAndConnect connects to an existing Yugabyte instance or creates a new one.
func StartAndConnect(ctx context.Context) (*Connection, error) {
	yugaInstance := getYugaInstanceType()

	var connOptions []*Connection
	switch yugaInstance {
	case yugaInstanceContainer:
		container := YugabyteDBContainer{}
		err := container.StartContainer(ctx)
		if err != nil {
			return nil, err
		}
		connOptions, err = container.getConnectionOptions(ctx)
		if err != nil {
			return nil, err
		}
	case yugaInstanceLocal:
		connOptions = []*Connection{
			NewConnection("localhost", yugaDBPort),
		}
	default:
		return nil, fmt.Errorf("unknown yuga instance type: %s", yugaInstance)
	}

	return WaitFirstReady(ctx, connOptions)
}
