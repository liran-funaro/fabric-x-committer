package test

import (
	"context"
	_ "embed"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	spec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature/sigtest"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

const (
	ordererImage     = "icr.io/cbdc/mock-ordering-service:0.0.2"
	testNodeImage    = "icr.io/cbdc/committer-test-node:0.0.2"
	ordererPort      = "7050"
	sidecarPort      = "5050"
	queryServicePort = "7001"
	channelName      = "testchannel"
	grpcConfig       = `{
	"healthCheckConfig": {
		"serviceName": ""
	},
	"methodConfig": [{
		"name": [{"service": ""}],
		"waitForReady": true,
		"retryPolicy": {
			"MaxAttempts": 5,
			"InitialBackoff": ".1s",
			"MaxBackoff": "1s",
			"BackoffMultiplier": 2.0,
			"RetryableStatusCodes": [ "UNAVAILABLE" ]
		}
	}]
}`
)

// TestStartTestNode spawns a mock orderer and an all-in-one instance of the committer using docker
// to verify that the committer container starts as expected.
func TestStartTestNode(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	require.NoError(t, err)
	testdataPath := path.Join(wd, "testdata")
	require.DirExists(t, testdataPath)

	ctx := t.Context()
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer connection.CloseConnectionsLog(dockerClient)

	stopAndRemoveContainersByName(ctx, t, dockerClient, "orderer", "committer")
	startOrderer(ctx, t, dockerClient, "orderer", testdataPath)
	startCommitter(ctx, t, dockerClient, "committer", testdataPath)

	// TODO: do some more checks
}

//nolint:revive
func startOrderer(ctx context.Context, t *testing.T, dockerClient *client.Client, name, testdataPath string) {
	t.Helper()
	imageName := ordererImage
	containerName := name
	servicePort := ordererPort

	// TODO: we want to replace this with `config/templates` in the future
	configPath := path.Join(testdataPath, "config-orderer.yaml")
	require.FileExists(t, configPath)

	containerCfg := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			nat.Port(servicePort + "/tcp"): struct{}{},
		},
		Cmd: []string{"/app", "start", "--configs=/config.yaml"},
		Tty: true,
	}

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   configPath,
				Target:   "/config.yaml",
				ReadOnly: true,
			},
		},
		PortBindings: nat.PortMap{
			nat.Port(servicePort + "/tcp"): []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: servicePort,
				},
			},
		},
	}

	startContainer(ctx, t, dockerClient, containerName, containerCfg, hostCfg, nil, nil, nil)
}

//nolint:revive
func startCommitter(ctx context.Context, t *testing.T, dockerClient *client.Client, name, testdataPath string) {
	t.Helper()
	imageName := testNodeImage
	containerName := name

	queryServiceEndpoint := ":" + queryServicePort
	ordererEndpoint := "host.docker.internal:" + ordererPort

	_, pubKey := sigtest.NewSignatureFactory(signature.Ecdsa).NewKeys()
	configBlockPath := configtempl.CreateConfigBlock(t, &configtempl.ConfigBlock{
		ChannelID: channelName,
		OrdererEndpoints: []*connection.OrdererEndpoint{
			{MspID: "org", Endpoint: *connection.CreateEndpoint(ordererEndpoint)},
		},
		MetaNamespaceVerificationKey: pubKey,
	})
	require.FileExists(t, configBlockPath)

	containerCfg := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			nat.Port(sidecarPort + "/tcp"):      struct{}{},
			nat.Port(queryServicePort + "/tcp"): struct{}{},
		},
		Env: []string{
			"SC_SIDECAR_ORDERER_CHANNEL_ID=" + channelName,
			"SC_SIDECAR_ORDERER_CONNECTION_ENDPOINTS=" + ordererEndpoint,
			"SC_QUERY_SERVICE_SERVER_ENDPOINT=" + queryServiceEndpoint,
		},
		Tty: true,
	}

	// TODO: we want to replace this with `config/templates` in the future
	configPath := filepath.Join(testdataPath, "config-sidecar.yaml")
	require.FileExists(t, configPath)

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{ // config block
				Type:     mount.TypeBind,
				Source:   configBlockPath,
				Target:   "/root/config/sc-genesis-block.proto.bin",
				ReadOnly: true,
			},
			{ // configuration
				Type:     mount.TypeBind,
				Source:   configPath,
				Target:   "/root/config/config-sidecar.yaml",
				ReadOnly: true,
			},
		},
		PortBindings: nat.PortMap{
			// sidecar port binding
			nat.Port(sidecarPort + "/tcp"): []nat.PortBinding{{
				HostIP:   "0.0.0.0",
				HostPort: sidecarPort,
			}},
			// query service port bindings
			nat.Port(queryServicePort + "/tcp"): []nat.PortBinding{{
				HostIP:   "0.0.0.0",
				HostPort: queryServicePort,
			}},
		},
	}

	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	t.Cleanup(cancel)

	options := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(grpcConfig),
	}

	endpoint := "localhost:" + sidecarPort
	conn, err := grpc.NewClient(endpoint, options...)
	require.NoError(t, err)

	startContainer(ctx, t, dockerClient, containerName, containerCfg, hostCfg, nil, nil, conn)
}

//nolint:revive
func startContainer(
	ctx context.Context,
	t *testing.T,
	dockerClient *client.Client,
	containerName string,
	containerCfg *container.Config,
	hostCfg *container.HostConfig,
	networkCfg *network.NetworkingConfig,
	platformCfg *spec.Platform,
	conn grpc.ClientConnInterface,
) {
	t.Helper()
	resp, err := dockerClient.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, platformCfg, containerName)
	require.NoError(t, err)

	//nolint:contextcheck // We want to ensure cleanup when the test is done.
	t.Cleanup(func() {
		stopAndRemoveID(context.Background(), t, dockerClient, resp.ID)
	})

	require.NoError(t, dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}))

	logs, err := dockerClient.ContainerLogs(ctx, resp.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
	})
	require.NoError(t, err)
	go func() {
		_, err = io.Copy(os.Stdout, logs)
		if err != nil {
			t.Logf("[%s] logs ended with: %v", containerName, err)
		}
	}()

	// return if we don't want wait for the health check to pass
	test.WaitUntilGrpcServerIsReady(ctx, t, conn)
}

func stopAndRemoveContainersByName(ctx context.Context, t *testing.T, dockerClient *client.Client, names ...string) {
	t.Helper()
	list, err := dockerClient.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	require.NoError(t, err)

	nameToID := make(map[string]string)
	for _, c := range list {
		for _, name := range c.Names {
			nameToID[name[1:]] = c.ID
		}
	}
	for _, containerName := range names {
		id, ok := nameToID[containerName]
		if !ok {
			t.Logf("container '%s' not found", containerName)
			continue
		}
		t.Logf("stopping container '%s' (%s)", containerName, id)
		stopAndRemoveID(ctx, t, dockerClient, id)
	}
}

func stopAndRemoveID(ctx context.Context, t *testing.T, dockerClient *client.Client, id string) {
	t.Helper()
	err := dockerClient.ContainerStop(ctx, id, container.StopOptions{})
	if err != nil {
		t.Logf("unable to stop container %s: %s", id, err)
	}
	err = dockerClient.ContainerRemove(ctx, id, container.RemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
	if err != nil {
		t.Logf("unable to remove container: %s", err)
	}
}
