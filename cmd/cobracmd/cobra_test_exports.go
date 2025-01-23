package cobracmd

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

// CommandTest is a struct that represents a CMD unit test.
type CommandTest struct {
	Name            string
	Args            []string
	CmdLoggerOutput string
	CmdStdOutput    string
	Err             error
	Endpoint        string
}

// PrepareTestDirs will create a temporary directories for the test.
func PrepareTestDirs(t *testing.T) (
	logPath string,
	testPath string,
) {
	tmpDir := t.TempDir()
	loggerOutputPath := filepath.Clean(path.Join(tmpDir, "logger-output.txt"))
	testConfigPath := filepath.Clean(path.Join(tmpDir, "test-config.yaml"))

	return loggerOutputPath, testConfigPath
}

// UnitTestRunner is a function that runs a unit test with the requested parameters passed in CommandTest structure.
func UnitTestRunner(
	t *testing.T,
	cmd *cobra.Command,
	loggerOutputPath string,
	test CommandTest,
) {
	// Set new logger with the config setup.
	logger := &logging.Config{
		Enabled:     true,
		Level:       logging.Debug,
		Caller:      true,
		Development: true,
		Output:      loggerOutputPath,
	}
	logging.SetupWithConfig(logger)
	t.Cleanup(func() {
		// Delete the logger's files of the current test.
		require.NoError(t, os.Remove(logger.Output))
	})

	// Creating new buffers for the cmd stdout and stderr.
	cmdStdOut := new(bytes.Buffer)
	cmdStdErr := new(bytes.Buffer)
	// Redirect of the cmd outputs errs.
	cmd.SetOut(cmdStdOut)
	cmd.SetErr(cmdStdErr)
	cmd.SetArgs(test.Args)

	wg := &sync.WaitGroup{}
	t.Cleanup(wg.Wait)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := cmd.ExecuteContextC(ctx)
		err = connection.WrapStreamRpcError(err)
		require.Equal(t, test.Err, err)
	}()

	// Require that the cmd output will align with the test expected output.
	require.Eventually(t, func() bool {
		return strings.Contains(cmdStdOut.String(), test.CmdStdOutput)
	}, 4*time.Second, 500*time.Millisecond)

	// Require that the logger's output will align with the test expected output.
	require.Eventually(t, func() bool {
		logOut, errFile := os.ReadFile(logger.Output)
		if errFile != nil {
			return false
		}
		return strings.Contains(string(logOut), test.CmdLoggerOutput) && strings.Contains(string(logOut), test.Endpoint)
	}, time.Minute, 500*time.Millisecond)
}
