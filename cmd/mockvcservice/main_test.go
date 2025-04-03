package main

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
)

//go:embed mockvcservice-test-config.yaml
var configTemplate string

//nolint:paralleltest // Cannot parallelize due to viper.
func TestMockVCServiceCmd(t *testing.T) {
	loggerOutputPath, testConfigPath := cobracmd.PrepareTestDirs(t)
	config := fmt.Sprintf(configTemplate, loggerOutputPath)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	// In some IDEs, using fmt.Sprintf() for test names can prevent the tests from being properly
	// identified. Instead, string concatenation is used for better compatibility.
	commonTests := []cobracmd.CommandTest{
		{
			Name:            "start the " + serviceName,
			Args:            []string{"start", "--configs", testConfigPath},
			CmdLoggerOutput: "Serving",
			CmdStdOutput:    fmt.Sprintf("Starting %v service", serviceName),
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
			cobracmd.UnitTestRunner(t, mockvcserviceCmd(), loggerOutputPath, tc)
		})
	}
}
