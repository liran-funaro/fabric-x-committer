package runner

import (
	"context"
	"os"
	"os/signal"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ##########################################
// # Helpers
// ##########################################

// cleanup makes sure the test ended gracefully and all containers have stopped and removed
func cleanup(t *testing.T, y *YugabyteDB) {
	exitErr := y.Stop()
	t.Logf("Exit error: %s", exitErr)

	// It takes some time to remove the container after it stopped
	time.Sleep(time.Second)

	// Verify that any yugabyte container with the chosen name is stopped and removed
	allContainers, err := y.Client.ListContainers(docker.ListContainersOptions{All: true})
	require.NoError(t, err)
	for _, c := range allContainers {
		for _, n := range c.Names {
			if !assert.NotEqual(t, n, y.Name, "container was not removed: %+v", c) {
				_ = y.Client.StopContainer(c.ID, 0)
				break
			}
		}
	}
}

// requireStop stops yugabyte and verify the exit error
func requireStop(t *testing.T, y *YugabyteDB, expectedCause error) {
	cause := y.Stop()
	require.Error(t, cause, "cause: %s", cause)
	require.Equal(t, cause, expectedCause)
}

// waitForContainer wait for the container to run
func waitForContainer(t *testing.T, y *YugabyteDB) {
	for running, err := y.IsContainerRunning(); !running || err != nil; running, err = y.IsContainerRunning() {
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond)
	}
}

// prepareYugaTestEnv creates YugabyteDB instance and adds cleanup hookups
func prepareYugaTestEnv(t *testing.T, ctx context.Context) *YugabyteDB {
	y := &YugabyteDB{
		Context:      ctx,
		OutputStream: os.Stdout,
		ErrorStream:  os.Stderr,
		Logf:         t.Logf,
	}
	t.Cleanup(func() { cleanup(t, y) })

	// Make sure we stop the container in case the test was killed
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	go func() {
		<-c
		cleanup(t, y)
	}()
	return y
}

// prepareYugaTestEnvWithTimeout is equivalent to prepareYugaTestEnv, but adds timeout to the test to avoid hangups
func prepareYugaTestEnvWithTimeout(t *testing.T, ctx context.Context, timeout time.Duration) *YugabyteDB {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	t.Cleanup(cancel)
	return prepareYugaTestEnv(t, ctx)
}

// prepareYugaTestEnvWithCancel is equivalent to prepareYugaTestEnv, but adds a cancellation option
func prepareYugaTestEnvWithCancel(t *testing.T, ctx context.Context) (*YugabyteDB, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return prepareYugaTestEnv(t, ctx), cancel
}

// ##########################################
// # Tests
// ##########################################

func Test_StartAndQuery(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)
	require.NoError(t, y.Start())

	conn, err := y.ConnectionSettings().Open()
	require.NoError(t, err)
	require.Nil(t, conn.Ping())

	rows, err := conn.Query("select distinct tablename from pg_catalog.pg_tables;")
	require.NoError(t, err)
	defer require.NoError(t, conn.Close())

	var allTables []string
	for rows.Next() {
		require.NoError(t, rows.Err())
		var tableName string
		require.NoError(t, rows.Scan(&tableName))
		allTables = append(allTables, tableName)
	}
	t.Logf("tables: %s", allTables)

	requireStop(t, y, Stopped)
}

func Test_InitTimeout(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)

	y.StartTimeout = time.Second
	err := y.Start()
	require.Error(t, err)

	require.Contains(t, err.Error(), "context deadline exceeded")
}

func Test_CancelBeforeStart(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	cancel()

	err := y.Start()
	require.Error(t, err, "err: %s", err)

	require.Equal(t, err, context.Canceled)
	requireStop(t, y, context.Canceled)
}

func Test_CancelAfterStart(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	y.StartBackground()
	cancel()

	requireStop(t, y, context.Canceled)
}

func Test_CancelAfterContainerStart(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	y.StartBackground()
	waitForContainer(t, y)
	cancel()

	requireStop(t, y, context.Canceled)
}

func Test_CancelAfterTestingConnections(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	y.StartBackground()
	waitForContainer(t, y)
	time.Sleep(2 * time.Second)
	cancel()

	requireStop(t, y, context.Canceled)
}

func Test_StopAfterStart(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)
	y.StartBackground()

	requireStop(t, y, Stopped)
}

func Test_StopAfterContainerStart(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)

	y.StartBackground()
	waitForContainer(t, y)

	requireStop(t, y, Stopped)
}

func Test_StopAfterTestingConnections(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)

	y.StartBackground()
	waitForContainer(t, y)
	time.Sleep(2 * time.Second)

	requireStop(t, y, Stopped)
}

func Test_CrashContainer(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)
	y.StartBackground()
	waitForContainer(t, y)
	y.stopContainer()

	err := y.Stop()
	require.Error(t, err, "cause: %s", err)
	require.Contains(t, err.Error(), "process exited")
}
