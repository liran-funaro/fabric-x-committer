package yuga

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	docker "github.com/fsouza/go-dockerclient"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// ClusterController is a class that facilitates the manipulation of YugaDB cluster,
// with its nodes running in Docker containers.
//
// To create the cluster, we are using Yugabyte's preconfigured tool, which is set up as follows:
// 1. If the cluster size is greater than or equal to 3,
// the replication factor (RF) is set to 3; otherwise, RF is set to 1.
// 2. The cluster supports a maximum of 3 master nodes.
// Therefore, a cluster with more than 3 master nodes cannot be created.
// Any additional node will act as a tserver node.
//
// In addition, RF=3 supports the failure of one master node, while RF=1 does not provide resilience to node failures.
// There are some bugs in YugaDB that prevent the cluster behavior from functioning as expected:
// 1. After a master-node is deleted, a new master-node cannot be added to the cluster as a replacement.
// 2. If the leader node is failing, the whole db connectivity will stop functioning.
type ClusterController struct {
	nodes []*YugabyteDBContainer
	lock  sync.RWMutex
}

// StartYugaCluster creates a Yugabyte cluster in a Docker environment and returns the first node connection properties.
func StartYugaCluster(ctx context.Context, t *testing.T, clusterSize uint) (*ClusterController, []*Connection) {
	t.Helper()
	require.GreaterOrEqual(t, clusterSize, uint(0))

	cluster := &ClusterController{}

	t.Logf("starting yuga cluster of size: %d", clusterSize)
	for range clusterSize {
		cluster.AddNode(ctx, t)
	}

	t.Cleanup(func() {
		cluster.stopAndRemoveYugaCluster(t)
	})

	return cluster, cluster.GetNodesConnections(ctx, t)
}

// AddNode creates, starts and add a YugabyteDB node to the cluster.
func (cc *ClusterController) AddNode(ctx context.Context, t *testing.T) *YugabyteDBContainer {
	t.Helper()
	cc.lock.Lock()
	defer cc.lock.Unlock()

	node := cc.createNode(ctx, t)
	cc.nodes = append(cc.nodes, node)

	node.StartContainer(ctx, t)
	cc.waitForNodeReadiness(t, node)

	return node
}

// waitForNodeReadiness checks the container's readiness by monitoring its logs.
func (*ClusterController) waitForNodeReadiness(t *testing.T, node *YugabyteDBContainer) {
	t.Helper()
	require.Eventually(
		t,
		func() bool {
			output := node.GetContainerLogs(t)
			return strings.Contains(output, "Data placement constraint successfully verified")
		},
		30*time.Second,
		250*time.Millisecond,
		"Node %s readiness check failed", node.Name,
	)
}

// RemoveLastNode stop and remove the last yugaDB node created, but not the first.
func (cc *ClusterController) RemoveLastNode(t *testing.T) {
	t.Helper()
	cc.lock.Lock()
	defer cc.lock.Unlock()
	clusterSize := len(cc.nodes)
	switch clusterSize {
	case 0:
		t.Logf("no nodes to remove.")

	case 1:
		t.Logf("only one node is alive. no follower nodes to remove.")

	default:
		stopAndRemoveDockerContainer(t, cc.nodes[clusterSize-1].containerID)
		cc.nodes = cc.nodes[:clusterSize-1]
	}
}

// GetClusterSize returns the amount of active nodes in the cluster.
func (cc *ClusterController) GetClusterSize() int {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	return len(cc.nodes)
}

// GetNodesConnections return a slice with all the cluster nodes connections.
func (cc *ClusterController) GetNodesConnections(ctx context.Context, t *testing.T) []*Connection {
	t.Helper()
	cc.lock.RLock()
	defer cc.lock.RUnlock()

	connections := make([]*Connection, len(cc.nodes))

	for i, node := range cc.nodes {
		connections[i] = node.getContainerConnection(ctx, t)
	}
	return connections
}

// createNode initializes a new node with appropriate configuration.
func (cc *ClusterController) createNode(ctx context.Context, t *testing.T) *YugabyteDBContainer {
	t.Helper()
	node := &YugabyteDBContainer{
		Name: fmt.Sprintf("yuga-%s", uuid.New()),
		Cmd:  yugabyteCMD,
	}

	if len(cc.nodes) != 0 {
		node.Cmd = append(node.Cmd, fmt.Sprintf("--join=%s", cc.getFirstNodeConnection(ctx, t).Host))
	}

	return node
}

func (cc *ClusterController) stopAndRemoveYugaCluster(t *testing.T) {
	t.Helper()
	cc.lock.Lock()
	defer cc.lock.Unlock()

	for _, node := range cc.nodes {
		t.Logf("stopping node: %v", node.Name)
		stopAndRemoveDockerContainer(t, node.containerID)
	}
	cc.nodes = make([]*YugabyteDBContainer, 0)
}

func (cc *ClusterController) getFirstNodeConnection(ctx context.Context, t *testing.T) *Connection {
	t.Helper()
	require.NotEmpty(t, cc.nodes)
	return cc.nodes[0].getContainerConnection(ctx, t)
}

// stopAndRemoveDockerContainer given a container name or ID.
func stopAndRemoveDockerContainer(t *testing.T, containerID string) {
	t.Helper()
	require.NoError(t, getDockerClient(t).StopContainer(containerID, 10))

	require.NoError(t, getDockerClient(t).RemoveContainer(docker.RemoveContainerOptions{
		ID:    containerID,
		Force: true,
	}))

	t.Logf("Container %s stopped and removed successfully", containerID)
}
