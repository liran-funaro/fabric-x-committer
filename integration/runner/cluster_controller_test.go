package runner

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestClusterController(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Container IP access not supported on non-linux Docker")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cc, _ := StartYugaCluster(ctx, t, 3)

	require.Equal(t, 3, cc.GetClusterSize())

	cc.AddNode(ctx, t)
	require.Equal(t, 4, cc.GetClusterSize())

	cc.RemoveLastNode(t)
	require.Equal(t, 3, cc.GetClusterSize())

	cc.stopAndRemoveYugaCluster(t)
	require.Equal(t, 0, cc.GetClusterSize())
}
