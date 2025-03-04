package test

import (
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

func TestLoadgenSidecar(t *testing.T) {
	gomega.RegisterTestingT(t)
	c := runner.NewRuntime(
		t,
		&runner.Config{
			NumSigVerifiers: 2,
			NumVCService:    2,
			BlockTimeout:    2 * time.Second,
			BlockSize:       500,
			LoadGen:         true,
		},
	)

	require.Eventually(t, func() bool {
		count := c.CountStatus(t, protoblocktx.Status_COMMITTED)
		t.Logf("count %d", count)
		return count > 1_000
	}, 90*time.Second, 3*time.Second)
	require.Zero(t, c.CountAlternateStatus(t, protoblocktx.Status_COMMITTED))
}
