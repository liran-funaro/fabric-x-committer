package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var configTemplate = `
validator-committer-service:
  server:
    endpoint:
      host: "localhost" # The host of the server
      port: 6002        # The port of the server
  database:
    host: %s                          # The host of the database
    port: %s                                 # The port of the database
    username: %s                       # The username for the database
    password: %s                       # The password for the database
    database: "yugabyte"                       # The database name
    max-connections: 10                        # The maximum size of the connection pool
    min-connections: 5                         # The minimum size of the connection pool.
  monitoring:
    metrics:
      endpoint: :2111
`

const (
	expectedStartUsage = "Usage:\n" +
		"  vcservice start [flags]\n\n" +
		"Flags:\n" +
		"      --configpath string   set the absolute path of config directory\n" +
		"  -h, --help                help for start\n\n"

	expectedVersionUsage = "Usage:\n" +
		"  vcservice version [flags]\n\n" +
		"Flags:\n" +
		"  -h, --help   help for version\n"

	testConfigFilePath = "./test-config.yaml"
)

func TestVCServiceCmd(t *testing.T) {
	dbRunner := &runner.YugabyteDB{}
	require.NoError(t, dbRunner.Start())
	t.Cleanup(func() {
		require.NoError(t, dbRunner.Stop())
	})

	conn := dbRunner.ConnectionSettings()
	config := fmt.Sprintf(configTemplate, conn.Host, conn.Port, conn.User, conn.Password)
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
			name:            "start the vcservice",
			args:            []string{"start", "--configpath", testConfigFilePath},
			cmdLoggerOutput: "Running server with",
			cmdStdOutput:    "Starting vcservice",
			errStr:          "",
			err:             nil,
		},
		{
			name:         "trailing args",
			args:         []string{"start", "arg1", "arg2"},
			cmdStdOutput: expectedStartUsage,
			errStr:       "Error: --configpath flag must be set to the path of configuration file\n",
			err:          errors.New("--configpath flag must be set to the path of configuration file"),
		},
		{
			name:         "print version",
			args:         []string{"version"},
			cmdStdOutput: "vcservice 0.2\n",
		},
		{
			name:         "trailing args",
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
		Output:      "./logger-output.txt",
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
				logOut, errFile := os.ReadFile(c.Output)
				require.NoError(t, errFile)
				return strings.Contains(string(logOut), tc.cmdLoggerOutput)
			}, 4*time.Second, 100*time.Millisecond)
			require.Equal(t, tc.errStr, cmdStdErr.String())
			require.Equal(t, tc.err, err)
			require.NoError(t, os.Remove(c.Output))
		})
	}
}
