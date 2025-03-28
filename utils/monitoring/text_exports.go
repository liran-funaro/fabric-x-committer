package monitoring

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

// WaitForConnections waits for a connection metric to have the required number of connected labels.
func WaitForConnections(t *testing.T, p *Provider, name string, requiredCount int) {
	t.Helper()
	require.Eventually(t, func() bool {
		gather, err := p.Registry().Gather()
		require.NoError(t, err)
		connectedCount := 0
		for _, mf := range gather {
			if mf.GetName() != name {
				continue
			}
			for _, m := range mf.GetMetric() {
				val := m.GetGauge().GetValue()
				if math.Abs(val-connection.Connected) < 1e-10 {
					connectedCount++
				}
			}
		}
		return connectedCount >= requiredCount
	}, time.Minute, 10*time.Millisecond)
}
