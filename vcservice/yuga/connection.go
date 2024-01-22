package yuga

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/pkg/errors"
	"github.com/yugabyte/pgx/v4/pgxpool"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("yuga connection")

const (
	defaultUsername = "yugabyte"
	defaultPassword = "yugabyte"

	stmtTemplateCreateDb       = "CREATE DATABASE %s;"
	stmtTemplateDropDbIfExists = "DROP DATABASE IF EXISTS %s;"
)

// Connection facilities connecting to a YugabyteDB instance.
type Connection struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// NewConnection returns a connection parameters with the specified host:port, and the default values
// for the other parameters.
func NewConnection(host, port string) *Connection {
	return &Connection{
		Host:     host,
		Port:     port,
		User:     defaultUsername,
		Password: defaultPassword,
	}
}

// DataSourceName returns the dataSourceName to be used by the database/sql package.
func (y *Connection) DataSourceName() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		y.User, y.Password, y.Host, y.Port, y.Database)
}

// AddressString returns the address:port as a string.
func (y *Connection) AddressString() string {
	return net.JoinHostPort(y.Host, y.Port)
}

// Open opens a connection pool to the database.
func (y *Connection) Open(ctx context.Context) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(y.DataSourceName())
	if err != nil {
		return nil, fmt.Errorf("[%s] error parsing datasource: %w", y.AddressString(), err)
	}

	poolConfig.MaxConns = 1
	poolConfig.MinConns = 1

	pool, err := pgxpool.ConnectConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("[%s] error making pool: %w", y.AddressString(), err)
	}
	return pool, err
}

// WaitFirstReady waits for a successful interaction with the database on one of the connections.
func WaitFirstReady(ctx context.Context, connOptions []*Connection) (*Connection, error) {
	reachableConn := make(chan *Connection)
	defer close(reachableConn)

	for _, conn := range connOptions {
		go func(c *Connection) {
			if c.WaitReady(ctx) {
				reachableConn <- c
			}
		}(conn)
	}

	select {
	case settings := <-reachableConn:
		return settings, nil
	case <-ctx.Done():
		err := ctx.Err()
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			err = errors.Wrap(err, "database is not ready")
		}
		return nil, err
	}
}

// WaitReady repeatably checks readiness until positive response arrives.
func (y *Connection) WaitReady(ctx context.Context) bool {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for !y.IsEndpointReady(ctx) {
		select {
		case <-ctx.Done():
			// Stop trying if the context cancelled
			return false
		case <-ticker.C:
		}
	}

	return true
}

// IsEndpointReady attempts to ping the database and returns true if successful.
func (y *Connection) IsEndpointReady(ctx context.Context) bool {
	conn, err := y.Open(ctx)
	if err != nil {
		logger.Debugf("[%s] error opening connection: %s", y.AddressString(), err)
		return false
	}
	defer conn.Close()

	if err = conn.Ping(ctx); err != nil {
		logger.Debugf("[%s] error pinging connection: %s", y.AddressString(), err)
		return false
	}
	logger.Infof("[%s] Connected to database", y.AddressString())
	return true
}

// CreateDB creates the database.
func (y *Connection) CreateDB(ctx context.Context, dbName string) error {
	pool, err := y.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err = execDropIfExitsDB(ctx, pool, dbName); err != nil {
		return err
	}

	return execCreateDB(ctx, pool, dbName)
}

// DropDB clears the database.
func (y *Connection) DropDB(ctx context.Context, dbName string) error {
	pool, err := y.Open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	return execDropIfExitsDB(ctx, pool, dbName)
}

// ExecRetry attempts to execute a statement multiple times until successful.
// This is a workaround for a known issues:
//   - Dropping a database with proximity to accessing it.
//     See: https://support.yugabyte.com/hc/en-us/articles/10552861830541-Unable-to-Drop-Database.
//   - Creating/dropping tables immediately after creating a database.
//     See: https://github.com/yugabyte/yugabyte-db/issues/14519.
func ExecRetry(ctx context.Context, pool *pgxpool.Pool, stmt string) error {
	var err error
	for i := 0; i < 60; i++ {
		if i > 0 {
			time.Sleep(time.Second)
		}
		_, err = pool.Exec(ctx, stmt)
		if err == nil {
			return nil
		}
		logger.Debugf("   > Failed exec attempt %d: %s", i+1, err)
	}
	return err
}

// execCreateDB creates a DB if exists given an existing pool.
func execCreateDB(ctx context.Context, pool *pgxpool.Pool, dbName string) error {
	logger.Infof("Creating database: %s", dbName)
	err := ExecRetry(ctx, pool, fmt.Sprintf(stmtTemplateCreateDb, dbName))
	if err != nil {
		return err
	}
	logger.Infof("Database created: %s", dbName)
	return nil
}

// execDropIfExitsDB drops a DB if exists given an existing pool.
func execDropIfExitsDB(ctx context.Context, pool *pgxpool.Pool, dbName string) error {
	logger.Infof("Dropping database if exists: %s", dbName)
	err := ExecRetry(ctx, pool, fmt.Sprintf(stmtTemplateDropDbIfExists, dbName))
	if err != nil {
		return err
	}
	logger.Infof("Database dropped: %s", dbName)
	return nil
}
