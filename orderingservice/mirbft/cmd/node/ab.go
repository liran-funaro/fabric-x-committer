package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/filecoin-project/mir"
	"github.com/filecoin-project/mir/pkg/events"
	"github.com/filecoin-project/mir/pkg/pb/requestpb"
	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
)

func newServerImpl(node *mir.Node, b chan *cb.Block) ab.AtomicBroadcastServer {
	s := &serverImpl{
		node:      node,
		blockChan: b,
		streams:   make([]ab.AtomicBroadcast_DeliverServer, 0),
		mu:        sync.RWMutex{},
	}
	s.startConsumeBlocks()
	return s
}

type serverImpl struct {
	node      *mir.Node
	blockChan chan *cb.Block

	streams []ab.AtomicBroadcast_DeliverServer
	mu      sync.RWMutex
}

func (s *serverImpl) startConsumeBlocks() {
	go func() {
		for {
			block := <-s.blockChan
			response := &ab.DeliverResponse{
				Type: &ab.DeliverResponse_Block{Block: block},
			}
			fmt.Printf("Received block %d. Sending to %d streams.\n", block.Header.Number, len(s.streams))

			s.mu.RLock()
			for _, stream := range s.streams {
				if err := stream.Send(response); err != nil {
					fmt.Printf("error sending block: %v\n", err)
				}
			}
			s.mu.RUnlock()
		}
	}()
}

// Broadcast receives a stream of messages from a client for ordering
func (s *serverImpl) Broadcast(srv ab.AtomicBroadcast_BroadcastServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())

	defer func(addr string) {
		fmt.Printf("connection closed with %s\n", addr)
	}(addr)

	for {
		envelope, err := srv.Recv()
		if err == io.EOF {
			fmt.Printf("Received EOF from %s, hangup\n", addr)
			return nil
		}
		if err != nil {
			fmt.Printf("Error reading from %s: %s\n", addr, err)
			return err
		}

		status, _ := s.broadcast(srv.Context(), envelope)

		err = srv.Send(&ab.BroadcastResponse{
			Status: status,
		})
		if status != cb.Status_SUCCESS || err != nil {
			return err
		}
	}
}

func (s *serverImpl) broadcast(_ context.Context, envelope *cb.Envelope) (cb.Status, error) {

	chdr, shdr, err := parseEnvelope(envelope)
	if err != nil {
		return cb.Status_BAD_REQUEST, nil
	}
	seqNo, _ := strconv.ParseUint(chdr.TxId, 0, 64)
	serializedEnvelope, _ := protoutil.Marshal(envelope)

	req := &requestpb.Request{
		ClientId: shdr.String(),
		ReqNo:    seqNo,
		Data:     serializedEnvelope,
	}

	// Submit the request to the Node.
	srErr := s.node.InjectEvents(context.TODO(), events.ListOf(events.NewClientRequests(
		"mempool",
		[]*requestpb.Request{req},
	)))
	if srErr != nil {
		fmt.Printf("error: %v", srErr)
		return cb.Status_INTERNAL_SERVER_ERROR, nil
	}

	return cb.Status_SUCCESS, nil
}

func parseEnvelope(envelope *cb.Envelope) (*cb.ChannelHeader, *cb.SignatureHeader, error) {
	payload, err := protoutil.UnmarshalPayload(envelope.Payload)
	if err != nil {
		return nil, nil, err
	}

	if payload.Header == nil {
		return nil, nil, errors.New("envelope has no header")
	}

	chdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		return nil, nil, err
	}

	shdr, err := protoutil.UnmarshalSignatureHeader(payload.Header.SignatureHeader)
	if err != nil {
		return nil, nil, err
	}

	return chdr, shdr, nil
}

// Deliver sends a stream of blocks to a client after ordering
func (s *serverImpl) Deliver(srv ab.AtomicBroadcast_DeliverServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())
	fmt.Printf("New deliver client connected from %s\n", addr)

	defer func(addr string) {
		fmt.Printf("connection closed with %s\n", addr)
	}(addr)

	for {
		if envelope, err := srv.Recv(); err == io.EOF {
			fmt.Printf("Received EOF from %s, hangup\n", addr)
			return nil
		} else if err != nil {
			fmt.Printf("Error reading from %s: %s\n", addr, err)
			return err
		} else if header, _, _ := parseEnvelope(envelope); header != nil {
			fmt.Printf("Added stream: %v\n", header)
		}

		s.mu.Lock()
		s.streams = append(s.streams, srv)
		s.mu.Unlock()
	}
}
