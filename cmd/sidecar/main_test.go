package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/coordinatormock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/orderermock"
)

//go:embed sidecar-cmd-test-config.yaml
var serverTemplate string

func TestSidecarCmd(t *testing.T) {
	ordererServerConfig, orderers, orderersGrpcServer := orderermock.StartMockOrderingService(1)
	t.Cleanup(func() {
		for _, o := range orderers {
			o.Close()
		}
		for _, oGrpc := range orderersGrpcServer {
			oGrpc.Stop()
		}
	})

	coordinatorServerConfig, coordinator, coordGrpcServer := coordinatormock.StartMockCoordinatorService()
	t.Cleanup(func() {
		coordinator.Close()
		coordGrpcServer.Stop()
	})

	loggerOutputPath, testConfigPath := cobracmd.PrepareTestDirs(t)
	ledgerPath := filepath.Clean(path.Join(t.TempDir(), "ledger"))
	config := fmt.Sprintf(
		serverTemplate,
		ordererServerConfig[0].Endpoint.Port,
		coordinatorServerConfig.Endpoint.Port,
		ledgerPath,
		3111,
		9111,
		loggerOutputPath,
	)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	// In some IDEs, using fmt.Sprintf() for test names can prevent the tests from being properly
	// identified. Instead, string concatenation is used for better compatibility.
	commonTests := []cobracmd.CommandTest{
		{
			Name:            "start the " + serviceName,
			Args:            []string{"start", "--configs", testConfigPath, "--endpoint", "localhost:8002"},
			CmdLoggerOutput: "Serving",
			CmdStdOutput:    fmt.Sprintf("Starting %v service", serviceName),
			Endpoint:        "localhost:8002",
		},
		{
			Name:         "print version",
			Args:         []string{"version"},
			CmdStdOutput: fmt.Sprintf("%v %v", serviceName, serviceVersion),
		},
		{
			Name: "trailing flag args for version",
			Args: []string{"version", "--test"},
			Err:  errors.New("unknown flag: --test"),
		},
		{
			Name: "trailing command args for version",
			Args: []string{"version", "test"},
			Err:  fmt.Errorf(`unknown command "test" for "%v version"`, serviceName),
		},
	}

	for _, test := range commonTests {
		tc := test
		t.Run(test.Name, func(t *testing.T) {
			cobracmd.UnitTestRunner(t, sidecarCmd(), loggerOutputPath, tc)
		})
	}
}
