package test

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

func TestDBNodeCrashHandling(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Container IP access not supported on non-linux Docker")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	t.Cleanup(cancel)

	clusterController, clusterConnection := runner.StartYugaCluster(ctx, t, 3)

	gomega.RegisterTestingT(t)
	c := runner.NewRuntime(t, &runner.Config{
		NumVerifiers: 2,
		NumVCService: 2,
		BlockTimeout: 2 * time.Second,
		BlockSize:    500,
		DBCluster:    clusterConnection,
	})
	c.Start(t, runner.FullTxPath)
	c.StartLoadGenOrderer(t)

	waitForCommittedTxs(t, c, 10_000)
	clusterController.RemoveLastNode(t)
	waitForCommittedTxs(t, c, 15_000)
}

func waitForCommittedTxs(t *testing.T, c *runner.CommitterRuntime, waitForCount int) {
	t.Helper()
	currentAmountOfTxs := c.CountStatus(t, protoblocktx.Status_COMMITTED)
	require.Eventually(t,
		func() bool {
			committedTxs := c.CountStatus(t, protoblocktx.Status_COMMITTED)
			t.Logf("Amount of committed txs: %d\n", committedTxs)
			return committedTxs > currentAmountOfTxs+waitForCount
		},
		90*time.Second,
		500*time.Millisecond,
	)
	require.Zero(t, c.CountAlternateStatus(t, protoblocktx.Status_COMMITTED))
}
