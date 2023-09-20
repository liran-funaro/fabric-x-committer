package main

import (
	"bytes"
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
sig-verification:
  server:
    endpoint:
      host: localhost
      port: 7001
`

func TestMockSigVerifierServiceCmd(t *testing.T) {
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
	cmd := mocksigverifierserviceCmd()

	cmdStdOut := new(bytes.Buffer)
	cmdStdErr := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetErr(cmdStdErr)
	cmd.SetArgs([]string{"start", "--configs", testConfigPath})

	go func() {
		_ = cmd.Execute()
	}()

	require.Eventually(t, func() bool {
		logOut, errFile := os.ReadFile(outputPath)
		if errFile != nil {
			return false
		}
		return strings.Contains(string(logOut), "Running server")
	}, 2*time.Second, 500*time.Millisecond)

	require.NoError(t, os.Remove(outputPath))
}
