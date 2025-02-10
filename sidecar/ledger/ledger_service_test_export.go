package ledger

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// EnsureAtLeastHeight checks if the ledger is at or above the specified height.
func EnsureAtLeastHeight(t *testing.T, s *Service, height uint64) {
	require.Eventually(t, func() bool {
		return s.GetBlockHeight() >= height
	}, 5*time.Second, 500*time.Millisecond)
}
