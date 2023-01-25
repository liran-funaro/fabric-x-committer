package main

import (
	"fmt"
	"sync"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/common/util"
)

type statelessDeliverServer struct {
	ab.UnimplementedAtomicBroadcastServer
	streams []peer.Deliver_DeliverServer
	mu      *sync.RWMutex
	input   chan *common.Block
}

func newStatelessDeliverService() deliverServer {
	i := &statelessDeliverServer{
		streams: make([]peer.Deliver_DeliverServer, 0),
		mu:      &sync.RWMutex{},
		input:   make(chan *common.Block, 100),
	}

	go func() {
		commonBlock := <-i.input
		fmt.Printf("Sending out block %v to %d clients\n", commonBlock.Header.Number, len(i.streams))
		response := &peer.DeliverResponse{Type: &peer.DeliverResponse_Block{Block: commonBlock}}
		i.mu.RLock()
		for _, stream := range i.streams {
			_ = stream.Send(response)
		}
		i.mu.RUnlock()
	}()
	return i
}

func (s *statelessDeliverServer) Input() chan<- *common.Block {
	return s.input
}

func (s *statelessDeliverServer) Deliver(srv peer.Deliver_DeliverServer) error {
	address := util.ExtractRemoteAddress(srv.Context())
	fmt.Printf("Opening new stream: %s\n", address)
	for {
		//TODO: We should normally read the request, because it defines the range of blocks the client wants to receive. However, we currently don't store the blocks and the statuses of their TXs.
		if _, err := srv.Recv(); err == nil {
			if s.findStreamIndex(srv) < 0 {
				fmt.Printf("Adding stream: %s\n", address)
				s.addStream(srv)
			} else {
				fmt.Printf("Cannot add stream: %s\n", address)
			}
		} else {
			fmt.Printf("Error occurred: %v\n", err)
			if index := s.findStreamIndex(srv); index >= 0 {
				fmt.Printf("Removing stream: %s\n", address)
				s.removeStream(index)
			} else {
				fmt.Printf("Could not remove stream: %s\n", address)
			}
			return err
		}
	}
}

func (s *statelessDeliverServer) addStream(stream peer.Deliver_DeliverServer) {
	s.mu.Lock()
	s.streams = append(s.streams, stream)
	s.mu.Unlock()
}

func (s *statelessDeliverServer) removeStream(index int) {
	s.mu.Lock()
	s.streams[index] = s.streams[len(s.streams)-1]
	s.streams = s.streams[:len(s.streams)-1]
	s.mu.Unlock()
}

func (s *statelessDeliverServer) findStreamIndex(needle peer.Deliver_DeliverServer) int {
	address := util.ExtractRemoteAddress(needle.Context())
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i, stream := range s.streams {
		if util.ExtractRemoteAddress(stream.Context()) == address {
			return i
		}
	}
	return -1
}
