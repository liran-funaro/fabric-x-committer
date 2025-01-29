package broadcastdeliver

import (
	"context"
	errors2 "errors"
	"fmt"
	"math"
	"strings"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
)

type (
	broadcastCft struct {
		broadcastCommon
		nodes []*broadcastNode
	}

	broadcastBft struct {
		broadcastCommon
		streams map[uint32]*broadcastCft
		quorum  int
	}

	broadcastCommon struct {
		// ctx nodes keep the context internally.
		ctx context.Context
	}

	broadcastNode struct {
		connNode
		parentCtx context.Context
		ctx       context.Context
		cancel    context.CancelFunc
		client    ab.AtomicBroadcastClient
		stream    ab.AtomicBroadcast_BroadcastClient
	}
)

func newBroadcastCft(
	ctx context.Context, connections []*ordererConnection, config *Config,
) *broadcastCft {
	connEndpoints := filterOrdererConnections(connections, Broadcast)
	connNodes := makeConnNodes(connEndpoints, config.Retry)
	// We shuffle the nodes for load balancing.
	shuffle(connNodes)
	nodes := make([]*broadcastNode, len(connNodes))
	for i, c := range connNodes {
		nodes[i] = &broadcastNode{
			connNode:  *c,
			parentCtx: ctx,
			client:    ab.NewAtomicBroadcastClient(c.connection),
		}
	}
	return &broadcastCft{
		broadcastCommon: broadcastCommon{ctx: ctx},
		nodes:           nodes,
	}
}

func newBroadcastBft(
	ctx context.Context, connections []*ordererConnection, config *Config,
) *broadcastBft {
	connectionMap := make(map[uint32][]*ordererConnection)
	for _, c := range connections {
		connectionMap[c.ID] = append(connectionMap[c.ID], c)
	}

	streams := make(map[uint32]*broadcastCft, len(connectionMap))
	for id, c := range connectionMap {
		streams[id] = newBroadcastCft(ctx, c, config)
	}

	n := len(streams)
	q, _ := computeBFTQuorum(n)
	return &broadcastBft{
		broadcastCommon: broadcastCommon{ctx: ctx},
		streams:         streams,
		quorum:          q,
	}
}

func (s *broadcastCommon) Context() context.Context {
	return s.ctx
}

func (s *broadcastCft) Submit(m *common.Envelope) (*ab.BroadcastResponse, error) {
	// We retry again for each call to submit, but we keep the previous order.
	for _, n := range s.nodes {
		n.reset()
	}
	for {
		node := s.nodes[0]
		if node.stop() {
			errs := make([]error, len(s.nodes))
			for i, n := range s.nodes {
				errs[i] = n.err
			}
			return nil, errors2.Join(errs...)
		}
		resp := node.submit(m)
		if node.err == nil {
			return resp, nil
		}
		sortNodes(s.nodes)
	}
}

func (s *broadcastBft) Submit(m *common.Envelope) (*ab.BroadcastResponse, error) {
	type response struct {
		id   uint32
		resp *ab.BroadcastResponse
		err  error
	}
	respQueue := channel.Make[response](s.ctx, len(s.streams))
	for id, stream := range s.streams {
		go func(id uint32, stream *broadcastCft) {
			r, err := stream.Submit(m)
			respQueue.Write(response{id: id, resp: r, err: err})
		}(id, stream)
	}

	var infoList []string
	var success int
	for len(infoList) < len(s.streams) {
		r, ok := respQueue.Read()
		if !ok {
			break
		}
		var info string
		switch {
		case r.resp != nil:
			if r.resp.Status == common.Status_SUCCESS {
				success++
			}
			info = r.resp.Status.String()
			if len(r.resp.Info) > 0 {
				info += fmt.Sprintf(": %s", r.resp.Info)
			}
		case r.err != nil:
			info = r.err.Error()
		default:
			info = "<nil>"
		}
		infoList = append(infoList, fmt.Sprintf("[%d] %s", r.id, info))
	}

	info := strings.Join(infoList, "\n")
	if success < s.quorum {
		return nil, errors.Errorf("insufficient quorum: %s", info)
	}
	return &ab.BroadcastResponse{
		Status: common.Status_SUCCESS,
		Info:   info,
	}, nil
}

func (s *broadcastNode) submit(m *common.Envelope) *ab.BroadcastResponse {
	s.err = s.wait(s.ctx)
	if s.err != nil {
		return nil
	}

	var resp *ab.BroadcastResponse
	// We try to create a new stream if we fail to send/receive.
	for range 2 {
		if !s.connect() {
			break
		}

		s.err = s.stream.Send(m)
		if s.err != nil {
			continue
		}
		resp, s.err = s.stream.Recv()
		if s.err == nil {
			break
		}
	}

	if s.err != nil {
		// Cancel the stream context to ensure resource release.
		s.cancel()
		s.backoff()
	} else {
		s.reset()
	}
	return resp
}

// connect returns true if successful.
func (s *broadcastNode) connect() bool {
	if s.parentCtx.Err() != nil {
		s.err = s.parentCtx.Err()
		return false
	}
	if s.err == nil && s.stream != nil && s.ctx != nil && s.ctx.Err() == nil {
		return true
	}
	if s.cancel != nil {
		// Ensure the previous stream context is cancelled.
		s.cancel()
	}
	s.ctx, s.cancel = context.WithCancel(s.parentCtx)
	s.stream, s.err = s.client.Broadcast(s.ctx)
	return s.err == nil
}

// Copied from the smartbft library.
// computeBFTQuorum calculates the quorums size Q, given a cluster size N.
//
// The calculation satisfies the following:
// Given a cluster size of N nodes, which tolerates f failures according to:
//
//	f = argmax ( N >= 3f+1 )
//
// Q is the size of the quorum such that:
//
//	any two subsets q1, q2 of size Q, intersect in at least f+1 nodes.
//
// Note that this is different from N-f (the number of correct nodes), when N=3f+3. That is, we have two extra nodes
// above the minimum required to tolerate f failures.
func computeBFTQuorum(n int) (q, f int) {
	f = (n - 1) / 3
	q = int(math.Ceil((float64(n) + float64(f) + 1) / 2.0))
	return q, f
}
