package monitoring

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
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

const randomName = ""

var logger = logging.New("monitoring")

type DockerRunOpts struct {
	RemoveIfExists bool
}

type DockerRunParams struct {
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

func (h *DockerHelper) Start(params *DockerRunParams, opts *DockerRunOpts) (string, error) {
	logger.Infof("Running docker for %s...", params.Hostname)
	reader, err := h.imagePull(params.Image)
	logger.Infof("Pulled docker image %s for %s.", params.Image, params.Hostname)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	if opts.RemoveIfExists {
		var removed int
		removed, err = h.removeExistingContainers(params.Image)
		logger.Infof("Removed %d containers of %s", removed, params.Image)
	}
	if err != nil {
		return "", err
	}

	containerId, err := h.createContainer(params)
	if err != nil {
		return "", err
	}

	err = h.startContainer(containerId)
	logger.Infof("Started container %s.", params.Hostname)
	if err != nil {
		return "", err
	}

	go h.containerLogs(containerId)
	logger.Infof("Enabled logs for %s.", params.Hostname)

	logger.Infof("Running %s is done!", params.Hostname)

	cntr, err := h.findContainerById(containerId)
	if err != nil {
		return "", err
	}
	return cntr.Names[0][1:], nil
}

func (h *DockerHelper) removeExistingContainers(imageName string) (int, error) {
	removed := 0
	containers, err := h.findContainersByImage(imageName)
	if err != nil {
		return removed, err
	}
	for _, cnt := range containers {
		if cnt.State == "running" {
			err = h.containerStop(cnt.ID)
			logger.Infof("Stopped container %v.", cnt.Names)
		}
		if err != nil {
			logger.Warnf("Failed to stop %v", cnt.Names)
		}
		err = h.containerRemove(cnt.ID)
		if err != nil {
			logger.Warnf("Failed to remove %v", cnt.Names)
		} else {
			removed++
		}
		logger.Infof("Removed and creating container %v.", cnt.Names)
	}
	return removed, nil
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

	cntr, err := h.client.ContainerCreate(h.ctx, &container.Config{
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
	}, &network.NetworkingConfig{}, nil, randomName)
	if err != nil {
		return "", err
	}
	return cntr.ID, nil
}

func (h *DockerHelper) findContainerById(containerId string) (*types.Container, error) {
	containers, err := h.client.ContainerList(h.ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	for _, c := range containers {
		if c.ID == containerId {
			return &c, nil
		}
	}
	return nil, nil
}

func (h *DockerHelper) findContainersByImage(imageName string) ([]types.Container, error) {
	containers, err := h.client.ContainerList(h.ctx, types.ContainerListOptions{All: true})
	if err != nil {
		return nil, err
	}
	result := make([]types.Container, 0, len(containers))
	for _, c := range containers {
		if c.Image == imageName {
			result = append(result, c)
		}
	}
	return result, nil
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

func (h *DockerHelper) startContainer(containerId string) error {
	return h.client.ContainerStart(h.ctx, containerId, types.ContainerStartOptions{})
}
