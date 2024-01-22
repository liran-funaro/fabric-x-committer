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
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice/yuga"
)

var configTemplate = `
logging:
  enabled: true
  level: debug
  Caller: true
  Development: true
  Output: %s
query-service:
  server:
    endpoint: localhost:7003
  database:
    host: %s
    port: %s
    username: %s
    password: %s
    database: %s
    max-connections: 10
    min-connections: 1
    load-balance: false
  monitoring:
    metrics:
      enable: true
      endpoint: localhost:7004
`

func TestQueryServiceCmd(t *testing.T) {
	conn := yuga.PrepareYugaTestEnv(t)

	tmpDir := t.TempDir()
	outputPath := filepath.Clean(path.Join(tmpDir, "logger-output.txt"))
	testConfigPath := filepath.Clean(path.Join(tmpDir, "test-config.yaml"))
	config := fmt.Sprintf(configTemplate,
		outputPath, conn.Host, conn.Port, conn.User, conn.Password, conn.Database)
	require.NoError(t, os.WriteFile(testConfigPath, []byte(config), 0o600))

	c := &logging.Config{
		Enabled:     true,
		Level:       logging.Debug,
		Caller:      true,
		Development: true,
		Output:      outputPath,
	}
	logging.SetupWithConfig(c)
	cmd := queryServiceCmd()

	cmdStdOut := new(bytes.Buffer)
	cmdStdErr := new(bytes.Buffer)
	cmd.SetOut(cmdStdOut)
	cmd.SetErr(cmdStdErr)
	cmd.SetArgs([]string{"start", "--configs", testConfigPath})

	errChan := make(chan error)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_, err := cmd.ExecuteContextC(ctx)
		errChan <- err
	}()

	require.Eventually(t, func() bool {
		logOut, errFile := os.ReadFile(outputPath)
		if errFile != nil {
			return false
		}
		return strings.Contains(string(logOut), "Running server")
	}, 2*time.Second, 500*time.Millisecond)
	cancel()

	require.NoError(t, <-errChan)

	require.NoError(t, os.Remove(outputPath))
}
