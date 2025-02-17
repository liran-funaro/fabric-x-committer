package broadcastdeliver

import (
	"context"
	"math/rand"
	"slices"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("broadcast-deliver")

type (
	connNode struct {
		connection            *grpc.ClientConn
		retry                 *backoff.ExponentialBackOff
		nextConnectionAttempt time.Time
		isStop                bool
		err                   error
	}

	ordererConnection struct {
		*connection.OrdererEndpoint
		*grpc.ClientConn
	}

	nodeReconnect interface {
		next() time.Time
		stop() bool
	}
)

func filterOrdererConnections(conn []*ordererConnection, api string) []*grpc.ClientConn {
	ret := make([]*grpc.ClientConn, 0, len(conn))
	for _, c := range conn {
		if c.SupportsAPI(api) {
			ret = append(ret, c.ClientConn)
		}
	}
	return ret
}

func makeConnNodes(connections []*grpc.ClientConn, retry *connection.RetryProfile) []*connNode {
	nodes := make([]*connNode, len(connections))
	for i, c := range connections {
		nodes[i] = &connNode{
			connection: c,
			retry:      retry.NewBackoff(),
		}
	}
	return nodes
}

func shuffle[T any](nodes []T) {
	rand.Shuffle(len(nodes), func(i, j int) { nodes[i], nodes[j] = nodes[j], nodes[i] })
}

func sortNodes[T nodeReconnect](nodes []T) {
	slices.SortStableFunc(nodes, func(e1, e2 T) int {
		if e1.stop() == e2.stop() {
			return e1.next().Compare(e2.next())
		}
		if e1.stop() {
			return 1
		}
		return -1
	})
}

func (c *connNode) next() time.Time {
	return c.nextConnectionAttempt
}

func (c *connNode) stop() bool {
	return c.isStop
}

func (c *connNode) backoff() {
	backoffTime := c.retry.NextBackOff()
	if backoffTime == c.retry.Stop {
		backoffTime = c.retry.MaxInterval
		c.isStop = true
	}
	c.nextConnectionAttempt = time.Now().Add(backoffTime)
}

func (c *connNode) reset() {
	c.retry.Reset()
	c.isStop = false
	c.nextConnectionAttempt = time.Time{}
	c.err = nil
}

func (c *connNode) wait(ctx context.Context) error {
	waitDuration := time.Until(c.next())
	if waitDuration > 0 {
		select {
		case <-time.After(waitDuration):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
