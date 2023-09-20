package main

import (
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

var (
	configTemplate = `
logging:
  enabled: true
  level: debug
  Caller: false
  Development: true
  Output: %s
coordinator-service:
  server:
    endpoint:
      host: "localhost"
      port: 3001
  sign-verifier:
    server:
      - # server 1 configuration
        endpoint:
          host: "localhost" # The host of the server
          port: %d        # The port of the server
  validator-committer:
    server:
      - # server 1 configuration
        endpoint:
          host: "localhost" # The host of the server
          port: %d        # The port of the server
  dependency-graph:
    num-of-local-dep-constructors: 20
    waiting-txs-limit: 10000
    num-of-workers-for-global-dep-manager: 20
  monitoring:
  per-channel-buffer-size-per-goroutine: 300
`

	testConfigFilePath = "./test-config.yaml"

	blockgenConfg = "../../config/config-blockgenforcoordinator.yaml"
)

func TestBlockGen(t *testing.T) {
	sigVerServerConfig, mockSigVer, sigVerGrpc := sigverifiermock.StartMockSVService(1)
	vcServerConfig, mockVC, vcGrpc := vcservicemock.StartMockVCService(1)

	t.Cleanup(func() {
		for _, sv := range mockSigVer {
			sv.Close()
		}

		for _, svGrpc := range sigVerGrpc {
			svGrpc.Stop()
		}

		for _, sv := range mockVC {
			sv.Close()
		}

		for _, svGrpc := range vcGrpc {
			svGrpc.Stop()
		}

		require.NoError(t, os.Remove(testConfigFilePath))
	})

	output := filepath.Clean(path.Join(t.TempDir(), "logger-output.txt"))
	conf := fmt.Sprintf(configTemplate, output, sigVerServerConfig[0].Endpoint.Port, vcServerConfig[0].Endpoint.Port)
	require.NoError(t, os.WriteFile(testConfigFilePath, []byte(conf), 0o600))

	require.NoError(t, config.ReadYamlConfigs([]string{testConfigFilePath}))
	coordConfig := coordinatorservice.ReadConfig()

	coordService := coordinatorservice.NewCoordinatorService(coordConfig)

	_, _, err := coordService.Start()
	require.NoError(t, err)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		connection.RunServerMain(coordConfig.ServerConfig, func(server *grpc.Server, port int) {
			if coordConfig.ServerConfig.Endpoint.Port == 0 {
				coordConfig.ServerConfig.Endpoint.Port = port
			}
			protocoordinatorservice.RegisterCoordinatorServer(server, coordService)
			wg.Done()
		})
	}()
	wg.Wait()

	cmd := blockgenCmd()
	cmdStdOut := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetArgs([]string{"start", "--configs", blockgenConfg})

	go func() {
		err = cmd.Execute()
	}()

	require.Eventually(t, func() bool {
		out := cmdStdOut.String()
		return strings.Contains(out, "blockgen started") &&
			strings.Contains(out, "Sending block to the coordinator service") &&
			strings.Contains(out, "Received status from the coordinator service")
	}, 4*time.Second, 100*time.Millisecond)
}
