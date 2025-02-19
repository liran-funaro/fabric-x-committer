package yuga

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	defaultImage        = "yugabytedb/yugabyte:2.20.7.0-b58"
	defaultInstanceName = "sc_yugabyte_unit_tests"
	defaultHostIP       = "127.0.0.1"
	defaultPortMap      = "7000/tcp"
)

// yugabyteCMD starts yugabyte without SSL and fault tolerance (single server).
var yugabyteCMD = []string{
	"bin/yugabyted", "start",
	"--callhome", "false",
	"--background", "false",
	"--ui", "false",
	"--tserver_flags", "ysql_max_connections=5000",
	"--insecure",
}

// YugabyteDBContainer manages the execution of an instance of a dockerized YugabyteDB for tests.
type YugabyteDBContainer struct {
	Name      string
	Image     string
	HostIP    string
	Network   string
	Cmd       []string
	HostPort  int
	DbPort    docker.Port
	PortMap   docker.Port
	PortBinds map[docker.Port][]docker.PortBinding
	NetToIP   map[string]*docker.EndpointConfig
	AutoRm    bool

	client      *docker.Client
	containerID string
}

// InitDefaults initialized default parameters.
func (y *YugabyteDBContainer) InitDefaults(t *testing.T) {
	t.Helper()
	if y.Image == "" {
		y.Image = defaultImage
	}

	if y.Name == "" {
		y.Name = defaultInstanceName
	}

	if y.HostIP == "" {
		y.HostIP = defaultHostIP
	}

	if y.DbPort == "" {
		y.DbPort = docker.Port(fmt.Sprintf("%s/tcp", yugaDBPort))
	}

	if y.Cmd == nil {
		y.Cmd = yugabyteCMD
	}

	if y.PortMap == "" {
		y.PortMap = defaultPortMap
	}

	if y.PortBinds == nil {
		y.PortBinds = map[docker.Port][]docker.PortBinding{
			y.PortMap: {{
				HostIP:   y.HostIP,
				HostPort: strconv.Itoa(y.HostPort),
			}},
		}
	}
	if y.client == nil {
		y.client = getDockerClient(t)
	}
}

// StartContainer runs a YugabyteDB container.
func (y *YugabyteDBContainer) StartContainer(ctx context.Context, t *testing.T) {
	t.Helper()
	y.InitDefaults(t)

	y.createContainer(ctx, t)

	// Starts the container
	err := y.client.StartContainerWithContext(y.containerID, nil, ctx)
	if _, ok := err.(*docker.ContainerAlreadyRunning); ok {
		t.Log("Container is already running")
		return
	}
	require.NoError(t, err)

	// Stream logs to stdout/stderr
	go y.streamLogs(t)
}

// createContainer attempts to create a container instance, or attach to an existing one.
func (y *YugabyteDBContainer) createContainer(ctx context.Context, t *testing.T) {
	t.Helper()
	// If container exists, we don't have to create it.
	found := y.findContainer(t)

	if found {
		return
	}

	// Pull the image if not exist
	require.NoError(t, y.client.PullImage(docker.PullImageOptions{
		Context:      ctx,
		Repository:   y.Image,
		OutputStream: os.Stdout,
	}, docker.AuthConfiguration{}))

	// Create the container instance
	container, err := y.client.CreateContainer(
		docker.CreateContainerOptions{
			Context: ctx,
			Name:    y.Name,
			Config: &docker.Config{
				Image: y.Image,
				Cmd:   y.Cmd,
			},
			HostConfig: &docker.HostConfig{
				AutoRemove:   y.AutoRm,
				PortBindings: y.PortBinds,
			},
		},
	)

	// If container created successfully, finish.
	if err == nil {
		y.containerID = container.ID
		return
	}
	require.ErrorIs(t, err, docker.ErrContainerAlreadyExists)

	// Try to find it again.
	require.True(t, y.findContainer(t), "cannot create container (already exists), but cannot find it")
}

// findContainer looks up a container with the same name.
func (y *YugabyteDBContainer) findContainer(t *testing.T) bool {
	t.Helper()
	allContainers, err := y.client.ListContainers(docker.ListContainersOptions{All: true})
	require.NoError(t, err, "could not load containers.")

	for _, c := range allContainers {
		for _, n := range c.Names {
			if n == y.Name || n == fmt.Sprintf("/%s", y.Name) {
				y.containerID = c.ID
				return true
			}
		}
	}

	return false
}

// getConnectionOptions inspect the container and fetches the available connection options.
func (y *YugabyteDBContainer) getConnectionOptions(ctx context.Context, t *testing.T) []*Connection {
	t.Helper()
	container, err := y.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		Context: ctx,
		ID:      y.containerID,
	})
	require.NoError(t, err)

	connOptions := []*Connection{
		NewConnection(container.NetworkSettings.IPAddress, y.DbPort.Port()),
	}
	for _, p := range container.NetworkSettings.Ports[y.DbPort] {
		connOptions = append(connOptions, NewConnection(p.HostIP, p.HostPort))
	}

	return connOptions
}

// getContainerConnection inspect the container and fetches its connection.
func (y *YugabyteDBContainer) getContainerConnection(ctx context.Context, t *testing.T) *Connection {
	t.Helper()
	container, err := y.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		Context: ctx,
		ID:      y.containerID,
	})
	require.NoError(t, err)

	return NewConnection(container.NetworkSettings.IPAddress, y.DbPort.Port())
}

// streamLogs streams the container output to the requested stream.
func (y *YugabyteDBContainer) streamLogs(t *testing.T) {
	t.Helper()
	logOptions := docker.LogsOptions{
		Context:      context.Background(),
		Container:    y.containerID,
		Follow:       true,
		ErrorStream:  os.Stderr,
		OutputStream: os.Stdout,
		Stderr:       true,
		Stdout:       true,
	}

	assert.NoError(t, y.client.Logs(logOptions))
}

// GetContainerLogs return the output of the YugabyteDBContainer.
func (y *YugabyteDBContainer) GetContainerLogs(t *testing.T) string {
	t.Helper()
	var outputBuffer bytes.Buffer
	require.NoError(t, y.client.Logs(docker.LogsOptions{
		Stdout:       true, // Capture standard output
		Container:    y.Name,
		OutputStream: &outputBuffer, // Capture in a string
	}))

	return outputBuffer.String()
}

func getDockerClient(t *testing.T) *docker.Client {
	t.Helper()
	client, err := docker.NewClientFromEnv()
	require.NoError(t, err)
	return client
}
