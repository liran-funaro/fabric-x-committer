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
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var configTemplate = `
logging:
  enabled: true
  level: debug
  Caller: true
  Development: true
  Output: %s
validator-committer-service:
  server:
    endpoint:
      host: localhost
      port: 6002
  database:
    host: %s
    port: %s
    username: %s
    password: %s
    database: yugabyte
    max-connections: 10
    min-connections: 1
    load-balance: false
  monitoring:
    metrics:
      endpoint: localhost:2111
`

const (
	expectedStartUsage = "Usage:\n" +
		"  vcservice start [flags]\n\n" +
		"Flags:\n" +
		"      --configs string   set the absolute path of config directory\n" +
		"  -h, --help             help for start\n\n"

	expectedVersionUsage = "Usage:\n" +
		"  vcservice version [flags]\n\n" +
		"Flags:\n" +
		"  -h, --help   help for version\n"
)

func TestVCServiceCmd(t *testing.T) {
	dbRunner := &runner.YugabyteDB{}
	require.NoError(t, dbRunner.Start())
	t.Cleanup(func() {
		require.NoError(t, dbRunner.Stop())
	})

	tmpDir := t.TempDir()
	outputPath := filepath.Clean(path.Join(tmpDir, "logger-output.txt"))
	testConfigPath := filepath.Clean(path.Join(tmpDir, "test-config.yaml"))
	conn := dbRunner.ConnectionSettings()
	config := fmt.Sprintf(configTemplate, outputPath, conn.Host, conn.Port, conn.User, conn.Password)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	test := []struct {
		name            string
		args            []string
		cmdLoggerOutput string
		cmdStdOutput    string
		errStr          string
		err             error
	}{
		{
			name:            "init the vcservice",
			args:            []string{"init", "--configs", testConfigPath, "--namespaces", "0,1"},
			cmdStdOutput:    "Initializing database",
			cmdLoggerOutput: "Table 'ns_1' is ready",
			errStr:          "",
			err:             nil,
		},
		{
			name:            "start the vcservice",
			args:            []string{"start", "--configs", testConfigPath},
			cmdLoggerOutput: "Running server",
			cmdStdOutput:    "Starting vcservice",
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
			cmdStdOutput: "vcservice 0.2\n",
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
		Level:       logging.Debug,
		Caller:      true,
		Development: true,
		Output:      outputPath,
	}

	for _, tc := range test {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			logging.SetupWithConfig(c)
			cmd := vcserviceCmd()

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
				logOut, errFile := os.ReadFile(outputPath)
				if errFile != nil {
					return false
				}
				return strings.Contains(string(logOut), tc.cmdLoggerOutput)
			}, 30*time.Second, 500*time.Millisecond)

			require.Equal(t, tc.errStr, cmdStdErr.String())
			require.Equal(t, tc.err, err)
			require.NoError(t, os.Remove(outputPath))
		})
	}
}
