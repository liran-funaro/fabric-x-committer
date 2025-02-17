package logging

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func setupTestLogger(t *testing.T) string {
	t.Helper()

	tmpFile, err := os.CreateTemp(t.TempDir(), "logtest")
	require.NoError(t, err)
	tmpFileName := tmpFile.Name()
	require.NoError(t, tmpFile.Close())

	config := &Config{
		Enabled:     true,
		Level:       Debug,
		Output:      tmpFileName,
		Development: true,
		Caller:      false,
	}
	SetupWithConfig(config)

	return tmpFileName
}

func TestLogOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		logMsg  string
		logFunc func(logger *Logger, msg string)
	}{
		{
			logMsg:  "info log",
			logFunc: func(l *Logger, msg string) { l.Info(msg) },
		},
		{
			logMsg:  "error log",
			logFunc: func(l *Logger, msg string) { l.Error(msg) },
		},
		{
			logMsg:  "debug log",
			logFunc: func(l *Logger, msg string) { l.Debug(msg) },
		},
	}

	for _, tc := range tests {
		t.Run(tc.logMsg, func(t *testing.T) {
			t.Parallel()
			tmpFileName := setupTestLogger(t)

			logger := New("test")
			tc.logFunc(logger, tc.logMsg)
			requireContains(t, tmpFileName, tc.logMsg)
		})
	}
}

func TestErrorStackTrace(t *testing.T) {
	t.Parallel()
	tmpFileName := setupTestLogger(t)

	logger := New("test")
	errMsg := "stack trace test error"
	testErr := errors.New(errMsg)
	logger.ErrorStackTrace(testErr)
	// ErrorStackTrace is the name of a function expected in the stack trace
	requireContains(t, tmpFileName, "ErrorStackTrace")
}

func requireContains(t *testing.T, fileName, contains string) {
	t.Helper()
	logData, err := os.ReadFile(fileName) //nolint:gosec // allow file name inclusion via variable
	require.NoError(t, err)
	require.Contains(t, string(logData), contains)
}
