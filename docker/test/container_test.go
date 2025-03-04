package test

import (
	"context"
	_ "embed"
	"log"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"

	"github.com/cockroachdb/errors"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	spec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
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

	// create a docker client
	ctx := t.Context()
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer func() {
		connection.CloseConnectionsLog(dockerClient)
	}()

	stopAndRemoveContainer(dockerClient, "orderer")
	stopAndRemoveContainer(dockerClient, "committer")

	// start orderer
	startOrderer(ctx, t, dockerClient, "orderer", testdataPath)

	// start committer
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

	containerCfg := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			nat.Port(servicePort + "/tcp"): struct{}{},
		},
		Cmd: []string{"/app", "start", "--configs=/config.yaml"},
	}

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: configPath,
				Target: "/config.yaml",
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

	sidecarPort := sidecarPort
	queryServicePort := queryServicePort

	channelName := channelName
	queryServiceEndpoint := ":" + queryServicePort
	ordererEndpoint := "host.docker.internal:" + ordererPort

	containerEnvOverride := []string{
		"SC_SIDECAR_ORDERER_CHANNEL_ID=" + channelName,
		"SC_SIDECAR_ORDERER_CONNECTION_ENDPOINTS=" + ordererEndpoint,
		"METANS_SIG_SCHEME=ECDSA",
		"METANS_SIG_VERIFICATION_KEY_PATH=/sc_pubkey.pem",
		"SC_QUERY_SERVICE_SERVER_ENDPOINT=" + queryServiceEndpoint,
	}

	containerCfg := &container.Config{
		Image: imageName,
		ExposedPorts: nat.PortSet{
			nat.Port(sidecarPort + "/tcp"):      struct{}{},
			nat.Port(queryServicePort + "/tcp"): struct{}{},
		},
		Env: containerEnvOverride,
	}

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				// configuration
				Type:   mount.TypeBind,
				Source: filepath.Join(testdataPath, "sc_pubkey.pem"),
				Target: "/sc_pubkey.pem",
			},
			{
				// configuration
				Type: mount.TypeBind,
				// TODO: we want to replace this with `config/templates` in the future
				Source: filepath.Join(testdataPath, "config-sidecar.yaml"),
				Target: "/root/config/config-sidecar.yaml",
			},
		},
		PortBindings: nat.PortMap{
			// sidecar port binding
			nat.Port(sidecarPort + "/tcp"): []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: sidecarPort,
				},
			},
			// query service port bindings
			nat.Port(queryServicePort + "/tcp"): []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: queryServicePort,
				},
			},
		},
	}

	endpoint := "localhost:" + sidecarPort

	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()

	options := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(grpcConfig),
	}

	conn, err := grpc.NewClient(endpoint, options...)
	require.NoError(t, err)

	startContainer(ctx, t, dockerClient, containerName, containerCfg, hostCfg, nil, nil, conn)
}

func startContainer(ctx context.Context, //nolint:revive
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

	require.NoError(t, dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}))

	// return if we don't want wait for the health check to pass
	if conn != nil {
		require.NoError(t, waitUntilReady(ctx, conn))
	}
}

func waitUntilReady(ctx context.Context, conn grpc.ClientConnInterface) error {
	healthClient := healthgrpc.NewHealthClient(conn)
	res, err := healthClient.Check(ctx, &healthgrpc.HealthCheckRequest{
		Service: "",
	}, grpc.WaitForReady(true))
	if status.Code(err) == codes.Canceled {
		return errors.Wrap(err, "healthcheck canceled")
	}
	if err != nil {
		return errors.Wrap(err, "healthcheck failed")
	}

	if res.Status != healthgrpc.HealthCheckResponse_SERVING {
		return errors.Wrap(err, "invalid status")
	}

	return nil
}

func stopAndRemoveContainer(dockerClient *client.Client, containerName string) {
	ctx := context.Background()

	if err := dockerClient.ContainerStop(ctx, containerName, container.StopOptions{}); err != nil {
		log.Printf("Unable to stop container %s: %s", containerName, err)
	}

	removeOptions := container.RemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	}

	if err := dockerClient.ContainerRemove(ctx, containerName, removeOptions); err != nil {
		log.Printf("Unable to remove container: %s", err)
	}
}
