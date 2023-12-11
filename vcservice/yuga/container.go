package yuga

import (
	"context"
	"fmt"
	"os"
	"strconv"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/pkg/errors"
)

const (
	defaultImage        = "yugabytedb/yugabyte:2.20.0.1-b1"
	defaultInstanceName = "sc_yugabyte_unit_tests"
	defaultHostIP       = "127.0.0.1"
)

// yugabyteCMD starts yugabyte without SSL and fault tolerance (single server).
var yugabyteCMD = []string{
	"bin/yugabyted", "start",
	"--advertise_address", "0.0.0.0",
	"--callhome", "false",
	"--fault_tolerance", "none",
	"--background", "false",
	"--ui", "false",
	"--tserver_flags", "ysql_max_connections=5000",
	"--insecure",
}

// YugabyteDBContainer manages the execution of an instance of a dockerized YugabyteDB for tests.
type YugabyteDBContainer struct {
	Name     string
	Image    string
	HostIP   string
	HostPort int
	DbPort   docker.Port
	Binds    []string

	client      *docker.Client
	containerID string
}

// InitDefaults initialized default parameters.
func (y *YugabyteDBContainer) InitDefaults() error {
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

	if y.client == nil {
		client, err := docker.NewClientFromEnv()
		if err != nil {
			return err
		}
		y.client = client
	}

	return nil
}

// StartContainer runs a YugabyteDB container.
func (y *YugabyteDBContainer) StartContainer(ctx context.Context) error {
	err := y.InitDefaults()
	if err != nil {
		return err
	}

	err = y.createContainer(ctx)
	if err != nil {
		return err
	}

	// Starts the container
	err = y.client.StartContainerWithContext(y.containerID, nil, ctx)
	if _, ok := err.(*docker.ContainerAlreadyRunning); ok {
		return nil
	} else if err != nil {
		return err
	}

	// Stream logs to stdout/stderr
	go y.streamLogs()

	return nil
}

// createContainer attempts to create a container instance, or attach to an existing one.
func (y *YugabyteDBContainer) createContainer(ctx context.Context) error {
	// If container exists, we don't have to create it.
	found, err := y.findContainer()
	if err != nil {
		return err
	}

	if found {
		return nil
	}

	// Pull the image if not exist
	err = y.client.PullImage(docker.PullImageOptions{
		Context:      ctx,
		Repository:   y.Image,
		OutputStream: os.Stdout,
	}, docker.AuthConfiguration{})
	if err != nil {
		return err
	}

	// Create the container instance
	container, err := y.client.CreateContainer(
		docker.CreateContainerOptions{
			Context: ctx,
			Name:    y.Name,
			Config: &docker.Config{
				Image: y.Image,
				Cmd:   yugabyteCMD,
			},
			HostConfig: &docker.HostConfig{
				AutoRemove: true,
				PortBindings: map[docker.Port][]docker.PortBinding{
					y.DbPort: {{
						HostIP:   y.HostIP,
						HostPort: strconv.Itoa(y.HostPort),
					}},
				},
				Binds: y.Binds,
			},
		},
	)

	// If container created successfully, finish.
	if err == nil {
		y.containerID = container.ID
		return nil
	} else if !errors.Is(err, docker.ErrContainerAlreadyExists) {
		return err
	}

	// Try to find it again.
	found, err = y.findContainer()
	if err != nil {
		return err
	}

	if found {
		return nil
	}

	return errors.New("cannot create container (already exists), but cannot find it")
}

// findContainer looks up a container with the same name.
func (y *YugabyteDBContainer) findContainer() (bool, error) {
	allContainers, err := y.client.ListContainers(docker.ListContainersOptions{All: true})
	if err != nil {
		return false, err
	}

	for _, c := range allContainers {
		for _, n := range c.Names {
			if n == y.Name || n == fmt.Sprintf("/%s", y.Name) {
				y.containerID = c.ID
				return true, nil
			}
		}
	}

	return false, nil
}

// getConnectionOptions inspect the container and fetches the available connection options.
func (y *YugabyteDBContainer) getConnectionOptions(ctx context.Context) ([]*Connection, error) {
	container, err := y.client.InspectContainerWithOptions(docker.InspectContainerOptions{
		Context: ctx,
		ID:      y.containerID,
	})
	if err != nil {
		return nil, err
	}

	connOptions := []*Connection{
		NewConnection(container.NetworkSettings.IPAddress, y.DbPort.Port()),
	}
	for _, p := range container.NetworkSettings.Ports[y.DbPort] {
		connOptions = append(connOptions, NewConnection(p.HostIP, p.HostPort))
	}

	return connOptions, nil
}

// streamLogs streams the container output to the requested stream.
func (y *YugabyteDBContainer) streamLogs() {
	logOptions := docker.LogsOptions{
		Context:      context.Background(),
		Container:    y.containerID,
		Follow:       true,
		ErrorStream:  os.Stderr,
		OutputStream: os.Stdout,
		Stderr:       true,
		Stdout:       true,
	}

	_ = y.client.Logs(logOptions)
}
