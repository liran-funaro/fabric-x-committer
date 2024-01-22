package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
    endpoint: localhost:6002
`

func TestMockVCServiceCmd(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Clean(path.Join(tmpDir, "logger-output.txt"))
	testConfigPath := filepath.Clean(path.Join(tmpDir, "test-config.yaml"))
	config := fmt.Sprintf(configTemplate, outputPath)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	c := &logging.Config{
		Enabled:     true,
		Level:       logging.Debug,
		Caller:      true,
		Development: true,
		Output:      outputPath,
	}
	logging.SetupWithConfig(c)
	cmd := mockvcserviceCmd()

	cmdStdOut := new(bytes.Buffer)
	cmdStdErr := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetErr(cmdStdErr)
	cmd.SetArgs([]string{"start", "--configs", testConfigPath})

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_, _ = cmd.ExecuteContextC(ctx)
	}()

	require.Eventually(t, func() bool {
		logOut, errFile := os.ReadFile(outputPath)
		if errFile != nil {
			return false
		}
		return strings.Contains(string(logOut), "Running server")
	}, 2*time.Second, 500*time.Millisecond)

	cancel()

	require.NoError(t, os.Remove(outputPath))
}
