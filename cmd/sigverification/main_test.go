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

//go:embed sigverification-cmd-test-config.yaml
var configTemplate string

func TestSigverifierCmd(t *testing.T) {
	loggerOutputPath, testConfigPath := cobracmd.PrepareTestDirs(t)
	config := fmt.Sprintf(
		configTemplate,
		50,
		"10ms",
		50,
		40,
		"Ecdsa",
		"localhost:5000",
		1,
		"localhost:2222",
		loggerOutputPath,
	)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	// In some IDEs, using fmt.Sprintf() for test names can prevent the tests from being properly
	// identified. Instead, string concatenation is used for better compatibility.
	commonTests := []cobracmd.CommandTest{
		{
			Name:            "start the " + serviceName,
			Args:            []string{"start", "--configs", testConfigPath, "--endpoint", "localhost:8001"},
			CmdLoggerOutput: "Serving",
			CmdStdOutput:    fmt.Sprintf("Starting %v service", serviceName),
			Endpoint:        "localhost:8001",
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
			cobracmd.UnitTestRunner(t, sigverifierCmd(), loggerOutputPath, tc)
		})
	}
}
