package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
)

//go:embed coordinator-cmd-test-config.yaml
var configTemplate string

//nolint:paralleltest // Cannot parallelize due to viper.
func TestCoordinatorServiceCmd(t *testing.T) {
	_, sigVerServers := mock.StartMockSVService(t, 1)
	_, vcServers := mock.StartMockVCService(t, 1)

	loggerOutputPath, testConfigPath := cobracmd.PrepareTestDirs(t)
	config := fmt.Sprintf(
		configTemplate,
		loggerOutputPath,
		sigVerServers.Configs[0].Endpoint.Port,
		vcServers.Configs[0].Endpoint.Port,
	)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	// In some IDEs, using fmt.Sprintf() for test names can prevent the tests from being properly
	// identified. Instead, string concatenation is used for better compatibility.
	commonTests := []cobracmd.CommandTest{
		{
			Name:            "start the " + serviceName,
			Args:            []string{"start", "--configs", testConfigPath, "--endpoint", "localhost:8004"},
			CmdLoggerOutput: "Serving",
			CmdStdOutput:    fmt.Sprintf("Starting %v service", serviceName),
			Endpoint:        "localhost:8004",
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
			cobracmd.UnitTestRunner(t, coordinatorserviceCmd(), loggerOutputPath, tc)
		})
	}
}
