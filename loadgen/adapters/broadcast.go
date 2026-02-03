/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package adapters

import (
	"context"
	"maps"
	"slices"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"google.golang.org/grpc"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/ordererconn"
)

type (
	// BroadcastStream implements broadcast, with the addition of automatically signing the envelope.
	BroadcastStream struct {
		broadcaster
	}

	// broadcaster can be either CFT or BFT broadcast.
	broadcaster interface {
		Send(*common.Envelope) error
		Close() error
	}

	//nolint:containedctx // grpc's stream contain context.
	cftBroadcaster struct {
		ctx context.Context

		conn         *grpc.ClientConn
		client       ab.AtomicBroadcastClient
		streamCtx    context.Context
		streamCancel context.CancelFunc
		stream       ab.AtomicBroadcast_BroadcastClient
	}

	// bftBroadcaster supports broadcasting to multiple nodes (one per ID).
	// It should be used only for testing as it does not validate successful broadcast to a valid quorum.
	bftBroadcaster struct {
		idToStream map[uint32]*cftBroadcaster
	}
)

// NewBroadcastStream starts a new broadcast stream (non-production).
func NewBroadcastStream(ctx context.Context, config *ordererconn.Config) (*BroadcastStream, error) {
	if err := ordererconn.ValidateConfig(config); err != nil {
		return nil, errors.Wrap(err, "error validating config")
	}

	switch config.FaultToleranceLevel {
	case ordererconn.CFT, ordererconn.NoFT:
		endpoints := ordererconn.GetEndpointsForAPI(config.Connection.Endpoints, ordererconn.Broadcast)
		b, err := newCFTBroadcaster(ctx, config, endpoints)
		if err != nil {
			return nil, err
		}
		return &BroadcastStream{broadcaster: b}, nil
	case ordererconn.BFT:
		endpointsPerID := ordererconn.GetEndpointsForAPIPerID(config.Connection.Endpoints, ordererconn.Broadcast)
		streams := make(map[uint32]*cftBroadcaster, len(endpointsPerID))
		for id, ep := range endpointsPerID {
			var err error
			streams[id], err = newCFTBroadcaster(ctx, config, ep)
			if err != nil {
				connection.CloseConnectionsLog(slices.Collect(maps.Values(streams))...)
				return nil, err
			}
		}
		return &BroadcastStream{broadcaster: &bftBroadcaster{
			idToStream: streams,
		}}, nil
	default:
		return nil, errors.Newf("invalid consensus type: '%s'", config.FaultToleranceLevel)
	}
}

func newCFTBroadcaster(
	ctx context.Context, config *ordererconn.Config, endpoints []*connection.Endpoint,
) (*cftBroadcaster, error) {
	conn, connErr := connection.NewLoadBalancedConnection(&connection.MultiClientConfig{
		Endpoints: endpoints,
		Retry:     config.Connection.Retry,
		TLS:       config.Connection.TLS,
	})
	if connErr != nil {
		return nil, connErr
	}
	return &cftBroadcaster{
		ctx:    ctx,
		conn:   conn,
		client: ab.NewAtomicBroadcastClient(conn),
	}, nil
}

// CloseConnections closes all the connections for the stream and logs errors.
func (s *BroadcastStream) CloseConnections() {
	connection.CloseConnectionsLog(s)
}

// SendBatch sends a batch one by one.
func (s *BroadcastStream) SendBatch(envelopes []*common.Envelope) error {
	for _, env := range envelopes {
		err := s.Send(env)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *bftBroadcaster) Close() error {
	broadcasters := slices.Collect(maps.Values(s.idToStream))
	err := make([]error, len(broadcasters))
	for i, b := range broadcasters {
		err[i] = b.Close()
	}
	return errors.Join(err...)
}

func (s *bftBroadcaster) Send(m *common.Envelope) error {
	errs := make([]error, 0, len(s.idToStream))
	for id, stream := range s.idToStream {
		err := stream.Send(m)
		if err != nil {
			errs = append(errs, errors.Wrapf(err, "failed to send message to stream [%d]", id))
		}
	}
	return errors.Join(errs...)
}

func (s *cftBroadcaster) Close() error {
	return s.conn.Close()
}

func (s *cftBroadcaster) Send(m *common.Envelope) error {
	var err error
	var isUpdate bool
	// We try twice. Once with the existing stream and second time with a new one.
	for !isUpdate {
		isUpdate, err = s.update()
		if err != nil {
			return err
		}
		err = s.stream.Send(m)
		if err == nil {
			return nil
		}
		// Force update.
		s.streamCancel()
	}
	return errors.Wrap(err, "failed to send message")
}

// update returns error if there is no alive connection.
func (s *cftBroadcaster) update() (bool, error) {
	if s.stream != nil && s.streamCtx != nil && s.streamCtx.Err() == nil {
		return false, nil
	}
	if s.streamCancel != nil {
		// Ensure the previous stream context is canceled.
		s.streamCancel()
	}
	//nolint:fatcontext // The previous streamCtx is cancelled prior to this.
	s.streamCtx, s.streamCancel = context.WithCancel(s.ctx)

	var err error
	s.stream, err = s.client.Broadcast(s.streamCtx)
	if err != nil {
		return true, err
	}

	// We load the parameter on stack before starting the go routine.
	workerCtx := s.streamCtx
	workerStream := s.stream
	workerCancel := s.streamCancel
	go func() {
		defer workerCancel()
		responseCollector(workerCtx, workerStream)
	}()
	return true, nil
}

func responseCollector(ctx context.Context, stream ab.AtomicBroadcast_BroadcastClient) {
	for ctx.Err() == nil {
		_, streamErr := stream.Recv()
		if streamErr != nil {
			return
		}
	}
}
