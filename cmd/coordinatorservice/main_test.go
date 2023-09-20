package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var configTemplate = `
logging:
  enabled: true
  level: info
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

const (
	expectedStartUsage = "Usage:\n" +
		"  coordinatorservice start [flags]\n\n" +
		"Flags:\n" +
		"      --configs string   set the absolute path of config directory\n" +
		"  -h, --help             help for start\n\n"

	expectedVersionUsage = "Usage:\n" +
		"  coordinatorservice version [flags]\n\n" +
		"Flags:\n" +
		"  -h, --help   help for version\n"

	testConfigFilePath = "./test-config.yaml"
)

func TestVCServiceCmd(t *testing.T) {
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
	})

	output := filepath.Clean(path.Join(t.TempDir(), "logger-output.txt"))
	config := fmt.Sprintf(configTemplate, output, sigVerServerConfig[0].Endpoint.Port, vcServerConfig[0].Endpoint.Port)
	require.NoError(t, os.WriteFile(testConfigFilePath, []byte(config), 0o600))

	t.Cleanup(func() {
		require.NoError(t, os.Remove(testConfigFilePath))
	})

	test := []struct {
		name            string
		args            []string
		cmdLoggerOutput string
		cmdStdOutput    string
		errStr          string
		err             error
	}{
		{
			name:            "start the coordinatorservice",
			args:            []string{"start", "--configs", testConfigFilePath},
			cmdLoggerOutput: "Coordinator service started successfully",
			cmdStdOutput:    "Starting coordinatorservice",
			errStr:          "",
			err:             nil,
		},
		{
			name:         "trailing args for start",
			args:         []string{"start", "arg1", "arg2"},
			cmdStdOutput: expectedStartUsage,
			errStr:       "Error: --configs flag must be set to the path of configuration file\n",
			err:          errors.New("--configs flag must be set to the path of configuration file"),
		},
		{
			name:         "print version",
			args:         []string{"version"},
			cmdStdOutput: "coordinatorservice 0.2\n",
		},
		{
			name:         "trailing args for version",
			args:         []string{"version", "arg1", "arg2"},
			cmdStdOutput: expectedVersionUsage + "\n",
			errStr:       "Error: trailing arguments detected\n",
			err:          errors.New("trailing arguments detected"),
		},
	}

	c := &logging.Config{
		Enabled:     true,
		Level:       logging.Info,
		Caller:      false,
		Development: true,
		Output:      output,
	}

	for _, tc := range test {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			logging.SetupWithConfig(c)
			cmd := coordinatorserviceCmd()

			cmdStdOut := new(bytes.Buffer)
			cmdStdErr := new(bytes.Buffer)
			cmd.SetOut(cmdStdOut)
			cmd.SetErr(cmdStdErr)
			cmd.SetArgs(tc.args)

			var err error
			go func() {
				err = cmd.Execute()
			}()

			require.Eventually(t, func() bool {
				return strings.Contains(cmdStdOut.String(), tc.cmdStdOutput)
			}, 4*time.Second, 100*time.Millisecond)

			require.Eventually(t, func() bool {
				logOut, errFile := os.ReadFile(output)
				if errFile != nil {
					return false
				}
				return strings.Contains(string(logOut), tc.cmdLoggerOutput)
			}, 4*time.Second, 100*time.Millisecond)
			require.Equal(t, tc.errStr, cmdStdErr.String())
			require.Equal(t, tc.err, err)
			require.NoError(t, os.Remove(output))
		})
	}
}
