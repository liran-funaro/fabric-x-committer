package runner

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestClusterController(t *testing.T) {
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

func TestClusterControllerConcurrency(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	t.Cleanup(cancel)

	cc, _ := StartYugaCluster(ctx, t, 0)

	clusterSize := 5

	g, gCtx := errgroup.WithContext(ctx)

	for range clusterSize {
		g.Go(func() error {
			cc.AddNode(gCtx, t)
			return nil
		})
	}
	require.NoError(t, g.Wait())

	for range clusterSize {
		g.Go(func() error {
			cc.RemoveLastNode(t)
			return nil
		})
	}
	require.NoError(t, g.Wait())

	require.Equal(t, 1, cc.GetClusterSize(), "Cluster size should be 1 after all follower nodes removals.")

	cc.stopAndRemoveYugaCluster(t)
	require.Equal(t, 0, cc.GetClusterSize(), "Cluster size should be 0 after all nodes removals.")
}
