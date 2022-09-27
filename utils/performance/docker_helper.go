package performance

import (
	"bufio"
	"context"
	"io"
	"os"
	"strconv"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("performance")

type DockerRunOpts struct {
	RemoveIfExists bool
}

type DockerRunParams struct {
	Name         string
	Image        string
	Hostname     string
	Envs         map[string]string
	Mounts       map[string]string
	Links        []string
	PortMappings map[int]int
}

type DockerHelper struct {
	ctx    context.Context
	client *client.Client
}

func (h *DockerHelper) Start(params *DockerRunParams, opts *DockerRunOpts) error {
	logger.Infof("Running docker for %s...", params.Hostname)
	reader, err := h.imagePull(params.Image)
	logger.Infof("Pulled docker image %s for %s.", params.Image, params.Hostname)
	if err != nil {
		return err
	}
	defer reader.Close()

	cnt, err := h.findContainerByName(params.Name)
	if err != nil {
		return err
	}
	if cnt != nil && cnt.State == "running" {
		logger.Infof("Container %s is already up and running.", params.Name)
		if opts.RemoveIfExists {
			utils.Must(h.containerStop(cnt.ID))
			logger.Infof("Stopped container %s.", params.Name)
		} else {
			return nil
		}
	}
	var containerId string
	if cnt == nil {
		containerId, err = h.createContainer(params)
		logger.Infof("Created container %s.", params.Hostname)
	} else if opts.RemoveIfExists {
		utils.Must(h.containerRemove(cnt.ID))
		logger.Infof("Removed container %s.", params.Hostname)
		containerId, err = h.createContainer(params)
		logger.Infof("Created container %s.", params.Hostname)
	} else {
		containerId = cnt.ID
	}
	if err != nil {
		return err
	}

	err = h.containerStart(containerId)
	logger.Infof("Started container %s.", params.Hostname)
	if err != nil {
		return err
	}

	go h.containerLogs(containerId)
	logger.Infof("Enabled logs for %s.", params.Hostname)

	logger.Infof("Running %s is done!", params.Hostname)
	return nil
}

func (h *DockerHelper) containerRemove(containerId string) error {
	return h.client.ContainerRemove(h.ctx, containerId, types.ContainerRemoveOptions{})
}

func (h *DockerHelper) containerStop(containerId string) error {
	return h.client.ContainerStop(h.ctx, containerId, nil)
}

func (h *DockerHelper) imagePull(image string) (io.ReadCloser, error) {
	reader, err := h.client.ImagePull(h.ctx, image, types.ImagePullOptions{})
	if err != nil {
		return nil, err
	}

	io.Copy(os.Stdout, reader)
	return reader, err
}

func DockerContainerRunner() (*DockerHelper, error) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &DockerHelper{
		ctx:    ctx,
		client: cli,
	}, nil
}
func (h *DockerHelper) createContainer(params *DockerRunParams) (string, error) {

	exposedPorts := nat.PortSet{}
	portBindings := nat.PortMap{}
	for internal, mapped := range params.PortMappings {
		tcpPort := nat.Port(strconv.Itoa(internal) + "/tcp")
		exposedPorts[tcpPort] = struct{}{}
		portBindings[tcpPort] = []nat.PortBinding{{
			HostIP:   "0.0.0.0",
			HostPort: strconv.Itoa(mapped),
		}}
	}

	var env []string
	for key, value := range params.Envs {
		env = append(env, key+"="+value)
	}
	var mounts []mount.Mount
	for source, target := range params.Mounts {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: source,
			Target: target,
		})
	}

	container, err := h.client.ContainerCreate(h.ctx, &container.Config{
		Hostname:     params.Hostname,
		Image:        params.Image,
		Tty:          true,
		Env:          env,
		ExposedPorts: exposedPorts,
	}, &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "always"},
		Links:         params.Links,
		Mounts:        mounts,
		PortBindings:  portBindings,
	}, &network.NetworkingConfig{}, nil, params.Name)
	if err != nil {
		return "", err
	}
	return container.ID, nil
}

func (h *DockerHelper) findContainerByName(containerName string) (*types.Container, error) {
	containers, err := h.client.ContainerList(h.ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				return &c, nil
			}
		}
	}
	return nil, nil
}

func (h *DockerHelper) containerLogs(containerId string) {
	reader, err := h.client.ContainerLogs(h.ctx, containerId, types.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		panic(err)
	}
	defer reader.Close()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		logger.Debug(scanner.Text())
	}
}

func (h *DockerHelper) containerStart(containerId string) error {
	return h.client.ContainerStart(h.ctx, containerId, types.ContainerStartOptions{})
}
