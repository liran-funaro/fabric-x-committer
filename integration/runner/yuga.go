package runner

import (
	"database/sql"
	"fmt"
	"net"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"context"
	"io"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/google/uuid"
	_ "github.com/lib/pq" // Importing the driver registers itself as being available to the database/sql package
	"github.com/pkg/errors"
	"github.com/tedsuo/ifrit"
)

const (
	// DefaultImage TODO: update to LTS/STS (v2.20) once the docker is released
	DefaultImage               = "yugabytedb/yugabyte:2.19.0.0-b190"
	DefaultUsername            = "yugabyte"
	DefaultPassword            = "yugabyte"
	DefaultSSLMode             = "disable"
	DefaultConnectionTimeout   = time.Second
	DefaultYugabyteDBPort      = "5433/tcp"
	DefaultHostIP              = "127.0.0.1"
	DefaultStartTimeout        = time.Minute
	DefaultContainerNamePrefix = "yugabyte-"
)

// yugabyteCMD starts yugabyte without SSL and fault tolerance (single server).
var yugabyteCMD = []string{
	"bin/yugabyted", "start",
	"--callhome", "false",
	"--fault_tolerance", "none",
	"--background", "false",
	"--insecure",
}

// fixedYugabytedURL Travis CI VM have an old kernel without the path `/sys/kernel/mm/transparent_hugepage/enabled`.
// This crashes the Yugabyte initiation script in the current docker version.
// The latest yugabyted script overcome this issue, but it is not released as a docker yet.
const fixedYugabytedURL = "https://raw.githubusercontent.com/yugabyte/yugabyte-db/961042a/bin/yugabyted"

// workaroundYugabyteCMD Downloads the fixed scripts before running it.
var workaroundYugabyteCMD = []string{
	"sh", "-c",
	fmt.Sprintf("curl -s '%s' > bin/yugabyted; %s", fixedYugabytedURL, strings.Join(yugabyteCMD, " ")),
}

// YugaConnectionSettings stores information for connecting to a YugabyteDB instance
type YugaConnectionSettings struct {
	Host              string
	Port              string
	User              string
	Password          string
	SSLMode           string
	ConnectionTimeout time.Duration
}

// DataSourceName returns the dataSourceName to be used by the database/sql package.
// Usage: sql.Open("postgres", y.DataSourceName())
func (y *YugaConnectionSettings) DataSourceName() string {
	return fmt.Sprintf("user=%s password=%s sslmode=%s host=%s port=%s connect_timeout=%.0f",
		y.User, y.Password, y.SSLMode, y.Host, y.Port, y.ConnectionTimeout.Seconds())
}

// AddressString returns the address:port as a string.
func (y *YugaConnectionSettings) AddressString() string {
	return net.JoinHostPort(y.Host, y.Port)
}

// Open is a wrapper for sql.Open.
func (y *YugaConnectionSettings) Open() (*sql.DB, error) {
	return sql.Open("postgres", y.DataSourceName())
}

// NewYugaConnectionSettings returns a connection parameters with the specified host:port, and the default values
// for the other parameters.
func NewYugaConnectionSettings(host string, port string) *YugaConnectionSettings {
	return &YugaConnectionSettings{
		Host:              host,
		Port:              port,
		User:              DefaultUsername,
		Password:          DefaultPassword,
		SSLMode:           DefaultSSLMode,
		ConnectionTimeout: DefaultConnectionTimeout,
	}
}

// YugabyteDB manages the execution of an instance of a dockerized YugabyteDB for tests.
type YugabyteDB struct {
	Context       context.Context
	Client        *docker.Client
	Image         string
	HostIP        string
	HostPort      int
	ContainerPort docker.Port
	Name          string
	StartTimeout  time.Duration
	Binds         []string
	ErrorStream   io.Writer
	OutputStream  io.Writer
	Logf          func(format string, args ...any)

	ctx           context.Context
	processCancel context.CancelCauseFunc
	creator       string
	process       ifrit.Process
	containerID   string
	connSettings  *YugaConnectionSettings
}

func (y *YugabyteDB) logF(format string, a ...any) {
	if y.Logf != nil {
		y.Logf(format, a...)
	} else if y.ErrorStream != nil {
		_, _ = fmt.Fprintf(y.ErrorStream, format+"\n", a...)
	} else if y.OutputStream != nil {
		_, _ = fmt.Fprintf(y.OutputStream, format+"\n", a...)
	}
}

// InitDefaults initialized default parameters.
func (y *YugabyteDB) InitDefaults() {
	if y.Context == nil {
		y.Context = context.Background()
	}

	if y.ctx == nil {
		y.ctx, y.processCancel = context.WithCancelCause(y.Context)
	}

	if y.Image == "" {
		y.Image = DefaultImage
	}

	if y.Name == "" {
		// The chance of a collision is low, so we can use lower cardinality for the name
		y.Name = fmt.Sprintf("%s%s", DefaultContainerNamePrefix, uuid.NewString()[:8])
	}

	if y.HostIP == "" {
		y.HostIP = DefaultHostIP
	}

	if y.ContainerPort == "" {
		y.ContainerPort = DefaultYugabyteDBPort
	}

	if y.StartTimeout == 0 {
		y.StartTimeout = DefaultStartTimeout
	}
}

// Stopped is the error returned by Wait() or Stop() when the process is stopped via Stop().
var Stopped = errors.New("process stopped by user")

// sigChanWaitContext returns a context that is canceled when a signal is given.
func (y *YugabyteDB) sigChanWaitContext(sigCh <-chan os.Signal) context.Context {
	ctx, cancel := context.WithCancelCause(y.ctx)
	go func() {
		select {
		case sig := <-sigCh:
			cancel(errors.Errorf("interrupted with signal: %s", sig.String()))
		}
	}()
	return ctx
}

func (y *YugabyteDB) errorOrCancelled(err error) error {
	if err == context.Canceled {
		<-y.ctx.Done()
		return context.Cause(y.ctx)
	}
	return err
}

// cleanContext makes sure the internal context of the processes is cancelled and cleared, so it won't be reused.
func (y *YugabyteDB) cleanContext() {
	if y.processCancel != nil {
		y.processCancel(nil)
	}

	y.ctx = nil
	y.processCancel = nil
}

// Run runs a YugabyteDB container. It implements the ifrit.Runner interface.
func (y *YugabyteDB) Run(sigCh <-chan os.Signal, ready chan<- struct{}) error {
	// We call InitDefaults() here in addition to StartBackground(). In case the user invokes the runner directly.
	y.InitDefaults()
	defer y.cleanContext()

	y.ctx = y.sigChanWaitContext(sigCh)

	if y.Client == nil {
		client, err := docker.NewClientFromEnv()
		if err != nil {
			return err
		}
		y.Client = client
	}

	// Pull the image if not exist
	err := y.Client.PullImage(docker.PullImageOptions{
		Context:      y.ctx,
		Repository:   y.Image,
		OutputStream: y.OutputStream,
	}, docker.AuthConfiguration{})
	if err != nil {
		return y.errorOrCancelled(err)
	}

	// Create the container instance
	container, err := y.Client.CreateContainer(
		docker.CreateContainerOptions{
			Context: y.ctx,
			Name:    y.Name,
			Config: &docker.Config{
				Image: y.Image,
				Env: []string{
					fmt.Sprintf("_creator=%s", y.creator),
				},
				// TODO: use yugabyteCMD when a new docker image is released
				Cmd: workaroundYugabyteCMD,
			},
			HostConfig: &docker.HostConfig{
				AutoRemove: true,
				PortBindings: map[docker.Port][]docker.PortBinding{
					y.ContainerPort: {{
						HostIP:   y.HostIP,
						HostPort: strconv.Itoa(y.HostPort),
					}},
				},
				Binds: y.Binds,
			},
		},
	)
	if err != nil {
		return y.errorOrCancelled(err)
	}
	y.containerID = container.ID
	defer y.stopContainer()

	// Starts the container
	if err = y.Client.StartContainerWithContext(y.containerID, nil, y.ctx); err != nil {
		return y.errorOrCancelled(err)
	}
	y.ctx = y.waitContainerContext()

	// Stream logs to stdout/stderr if available
	go y.streamLogs()

	// Fetch connection settings
	container, err = y.inspectContainer()
	if err != nil {
		return y.errorOrCancelled(err)
	}
	cPort := container.NetworkSettings.Ports[y.ContainerPort][0]

	// Wait for a successful interaction with the database
	y.connSettings, err = y.waitUntilReady([]*YugaConnectionSettings{
		NewYugaConnectionSettings(cPort.HostIP, cPort.HostPort),
		NewYugaConnectionSettings(container.NetworkSettings.IPAddress, y.ContainerPort.Port()),
	})
	if err != nil {
		return y.errorOrCancelled(err)
	}

	// Indicate readiness
	close(ready)

	<-y.ctx.Done()
	return context.Cause(y.ctx)
}

// stopContainer attempt to stop the container, logging errors.
func (y *YugabyteDB) stopContainer() {
	// We don't use context here because we want to be able to stop the container even when the context is cancelled
	if err := y.Client.StopContainer(y.containerID, 0); err != nil {
		y.logF("Failed stopping container: %s", err)
	}
}

func (y *YugabyteDB) inspectContainer() (*docker.Container, error) {
	return y.Client.InspectContainerWithOptions(docker.InspectContainerOptions{
		Context: y.ctx,
		ID:      y.containerID,
	})
}

// waitUntilReady waits for a successful interaction with the database.
func (y *YugabyteDB) waitUntilReady(connOptions []*YugaConnectionSettings) (*YugaConnectionSettings, error) {
	ctx, cancel := context.WithTimeout(y.ctx, y.StartTimeout)
	defer cancel()

	reachableConn := make(chan *YugaConnectionSettings)
	for _, conn := range connOptions {
		go y.readyChan(ctx, conn, reachableConn)
	}

	select {
	case settings := <-reachableConn:
		return settings, nil
	case <-ctx.Done():
		err := ctx.Err()
		if err == context.DeadlineExceeded {
			err = errors.Wrapf(err, "database in container '%s' is not ready", y.containerID)
		}
		return nil, err
	}
}

// readyChan repeatably checks readiness until positive response arrives.
func (y *YugabyteDB) readyChan(
	ctx context.Context, connSettings *YugaConnectionSettings, reachableConn chan *YugaConnectionSettings,
) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for !y.isEndpointReady(ctx, connSettings) {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			// Stop trying if the context cancelled
			return
		}
	}

	// We only reach here if the endpoint is ready
	reachableConn <- connSettings
}

// isEndpointReady attempts to ping the database and returns true if successful.
func (y *YugabyteDB) isEndpointReady(ctx context.Context, connSettings *YugaConnectionSettings) bool {
	conn, err := connSettings.Open()
	if err != nil {
		y.logF("[%s] field opening connection: %s", connSettings.AddressString(), err)
		return false
	}
	if err = conn.PingContext(ctx); err != nil {
		y.logF("[%s] failed pinging connection: %s", connSettings.AddressString(), err)
		return false
	}
	if err = conn.Close(); err != nil {
		y.logF("[%s] failed closing connection: %s", connSettings.AddressString(), err)
		return false
	}
	y.logF("[%s] success", connSettings.AddressString())
	return true
}

// errorOrWrap Wraps an error, or returns a new error if no error is provided.
func errorOrWrap(err error, format string, a ...any) error {
	if err == nil {
		return errors.Errorf(format, a...)
	}
	return errors.Wrapf(err, format, a...)
}

// waitContainerContext returns a context that is canceled when the container is done.
func (y *YugabyteDB) waitContainerContext() context.Context {
	ctx, cancel := context.WithCancelCause(y.ctx)
	go func() {
		exitCode, err := y.Client.WaitContainer(y.containerID)
		if exitCode != 0 {
			err = errorOrWrap(err, "'yugabytedb' process exited with code %d", exitCode)
		}
		cancel(err)
	}()
	return ctx
}

// streamLogs streams the container output to the requested stream.
func (y *YugabyteDB) streamLogs() {
	if y.ErrorStream == nil && y.OutputStream == nil {
		return
	}

	logOptions := docker.LogsOptions{
		Context:      y.ctx,
		Container:    y.containerID,
		Follow:       true,
		ErrorStream:  y.ErrorStream,
		OutputStream: y.OutputStream,
		Stderr:       y.ErrorStream != nil,
		Stdout:       y.OutputStream != nil,
	}

	if err := y.Client.Logs(logOptions); err != nil {
		y.logF("Log stream ended: %s", err)
	}
}

// ConnectionSettings returns the connection settings successfully used by the readiness check.
func (y *YugabyteDB) ConnectionSettings() *YugaConnectionSettings {
	return y.connSettings
}

// ContainerID returns the container ID.
func (y *YugabyteDB) ContainerID() string {
	return y.containerID
}

// StartBackground starts the container using an ifrit runner, without waiting for readiness.
// This call should follow by Ready(), Wait(), or ReadyOrError().
func (y *YugabyteDB) StartBackground() {
	// We also initiate the context here to allow Stop() to cancel the context even if the process haven't started yet.
	// If the user invokes the process directly (`ifrit.Background(y)`) and calls `Stop()` immediately,
	// then the exit cause returned by `Wait()` or `Stop()` might not be consistent.
	y.InitDefaults()
	y.creator = string(debug.Stack())
	y.process = ifrit.Background(y)
}

// Start starts the container using an ifrit runner and waits for readiness or error.
func (y *YugabyteDB) Start() error {
	y.StartBackground()
	return y.ReadyOrError()
}

// Ready returns a channel which will close once the runner is active.
func (y *YugabyteDB) Ready() <-chan struct{} {
	if y.process == nil {
		return nil
	}
	return y.process.Ready()
}

// Wait returns a channel that will emit a single error once the Process exits.
func (y *YugabyteDB) Wait() <-chan error {
	if y.process == nil {
		return nil
	}
	return y.process.Wait()
}

// ReadyOrError returns nil if ready, or error if stopped indicating the stop reason. Blocks otherwise.
func (y *YugabyteDB) ReadyOrError() error {
	if y.process == nil {
		return nil
	}
	select {
	case <-y.Ready():
		return nil
	case err := <-y.Wait():
		return err
	}
}

// Signal sends a shutdown signal to the Process. It does not block.
func (y *YugabyteDB) Signal(sig os.Signal) {
	if y.process == nil {
		return
	}
	y.process.Signal(sig)
}

// Stop the runner process and wait for completion, returning the exit error.
func (y *YugabyteDB) Stop() error {
	if y.process == nil {
		return nil
	}
	// First, cancel the context with explicit Stopped cause
	if y.processCancel != nil {
		y.processCancel(Stopped)
	}
	// Then, signal the process to stop
	y.Signal(os.Interrupt)
	return <-y.Wait()
}

// IsContainerRunning tests if the container is running
func (y *YugabyteDB) IsContainerRunning() (bool, error) {
	if y.containerID == "" {
		return false, nil
	}

	container, err := y.inspectContainer()
	if err != nil {
		return false, err
	}
	return container.State.Running, nil
}
