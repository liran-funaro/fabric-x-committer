package test

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

func TestLoadGenOrderer(t *testing.T) {
	t.Parallel()
	gomega.RegisterTestingT(t)
	c := runner.NewRuntime(t, &runner.Config{
		NumVerifiers: 2,
		NumVCService: 2,
		BlockTimeout: 2 * time.Second,
		BlockSize:    500,
	})
	c.StartSystem(t, runner.All)
	c.StartLoadGenOrderer(t)

	require.Eventually(t, func() bool {
		count := c.CountStatus(t, protoblocktx.Status_COMMITTED)
		t.Logf("count %d", count)
		return count > 1_000
	}, 90*time.Second, 3*time.Second)
	require.Zero(t, c.CountAlternateStatus(t, protoblocktx.Status_COMMITTED))
}

func TestLoadGenCommitter(t *testing.T) {
	t.Parallel()
	gomega.RegisterTestingT(t)
	c := runner.NewRuntime(t, &runner.Config{
		NumVerifiers: 2,
		NumVCService: 2,
		BlockTimeout: 2 * time.Second,
		BlockSize:    500,
	})
	c.StartLoadGenCommitter(t)
	c.StartSystem(t, runner.All-runner.Orderer)

	require.Eventually(t, func() bool {
		count := c.CountStatus(t, protoblocktx.Status_COMMITTED)
		t.Logf("count %d", count)
		return count > 1_000
	}, 90*time.Second, 500*time.Millisecond)
	require.Zero(t, c.CountAlternateStatus(t, protoblocktx.Status_COMMITTED))
}
