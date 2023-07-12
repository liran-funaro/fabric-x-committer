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

// findContainerUnderTest returns a container object if the container-under-test name was found.
func findContainerUnderTest(t *testing.T, y *YugabyteDB) *docker.APIContainers {
	allContainers, err := y.Client.ListContainers(docker.ListContainersOptions{All: true})
	require.NoError(t, err)
	for _, c := range allContainers {
		for _, n := range c.Names {
			if n == y.Name {
				return &c
			}
		}
	}

	return nil
}

// cleanup makes sure the test ended gracefully and all containers have stopped and removed
func cleanup(t *testing.T, y *YugabyteDB) {
	exitErr := y.Stop()
	t.Logf("Exit error: %s", exitErr)

	// Verify that any yugabyte container with the chosen name is stopped and removed
	isRemoved := assert.Eventually(t, func() bool {
		return findContainerUnderTest(t, y) == nil
	}, time.Minute, time.Second)

	// Force removal in case of problems
	if !isRemoved {
		if c := findContainerUnderTest(t, y); c != nil {
			t.Logf("forcing container termination: %+v", c)
			_ = y.Client.StopContainer(c.ID, 0)
		}
	}
}

// requireStop waits for yugabyte to terminate and verify the exit error
func requireDoneCause(t *testing.T, y *YugabyteDB, expectedCause error) {
	cause := <-y.Wait()
	require.Error(t, cause, "cause: %s", cause)
	require.Equal(t, expectedCause, cause)
}

// requireDoneCauseContains waits for yugabyte to terminate and verify the exit error
func requireDoneCauseContains(t *testing.T, y *YugabyteDB, expectedCauseMessage string) {
	cause := <-y.Wait()
	require.Error(t, cause, "cause: %s", cause)
	require.Contains(t, cause.Error(), expectedCauseMessage)
}

// requireStop stops yugabyte and verify the exit error
func requireStop(t *testing.T, y *YugabyteDB, expectedCause error) {
	assert.NoError(t, y.Stop())
	requireDoneCause(t, y, expectedCause)
}

// waitForContainer wait for the container to run
func waitForContainer(t *testing.T, y *YugabyteDB) {
	require.Eventually(t, func() bool {
		running, err := y.IsContainerRunning()
		require.NoError(t, err)
		return running
	}, time.Minute, 100*time.Millisecond, "container didn't start")
}

// waitForConnectionTest wait for the runner to start testing the different connection settings
func waitForConnectionTest(t *testing.T, y *YugabyteDB) {
	require.Eventually(t, func() bool {
		return y.containerConnectionTestInitiatedFlag
	}, time.Minute, 100*time.Millisecond, "container connection test didn't start")
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

	require.Equal(t, context.Canceled, err)
	requireDoneCause(t, y, context.Canceled)
}

func Test_CancelAfterStart(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	y.StartBackground()
	cancel()

	requireDoneCause(t, y, context.Canceled)
}

func Test_CancelAfterContainerStart(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	y.StartBackground()
	waitForContainer(t, y)
	cancel()

	requireDoneCause(t, y, context.Canceled)
}

func Test_CancelAfterTestingConnections(t *testing.T) {
	t.Parallel()
	y, cancel := prepareYugaTestEnvWithCancel(t, context.Background())
	y.StartBackground()
	waitForConnectionTest(t, y)
	cancel()

	requireDoneCause(t, y, context.Canceled)
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
	waitForConnectionTest(t, y)

	requireStop(t, y, Stopped)
}

func Test_InterruptAfterStart(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)
	y.StartBackground()
	y.Signal(os.Interrupt)

	requireDoneCauseContains(t, y, "interrupt")
}

func Test_InterruptAfterContainerStart(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)

	y.StartBackground()
	waitForContainer(t, y)
	y.Signal(os.Interrupt)

	requireDoneCauseContains(t, y, "interrupt")
}

func Test_InterruptAfterTestingConnections(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)

	y.StartBackground()
	waitForConnectionTest(t, y)
	y.Signal(os.Interrupt)

	requireDoneCauseContains(t, y, "interrupt")
}

func Test_CrashContainer(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)
	y.StartBackground()
	waitForContainer(t, y)
	y.stopContainer()

	requireDoneCauseContains(t, y, "process exited")
}

func Test_CrashContainerAfterTestingConnections(t *testing.T) {
	t.Parallel()
	y := prepareYugaTestEnvWithTimeout(t, context.Background(), 5*time.Minute)
	y.StartBackground()
	waitForConnectionTest(t, y)
	y.stopContainer()

	requireDoneCauseContains(t, y, "process exited")
}
