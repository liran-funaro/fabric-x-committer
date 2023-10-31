package main

import (
	"io"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/common/util"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

const capacity = 111

type MockOrderer struct {
	inEnvs    chan<- *common.Envelope
	outBlocks <-chan *common.Block
	stop      chan any
}

func NewSharedMockOrderer(inEnvs chan<- *common.Envelope, outBlocks <-chan *common.Block) *MockOrderer {
	return &MockOrderer{
		inEnvs:    inEnvs,
		outBlocks: outBlocks,
		stop:      make(chan any),
	}
}

func (o *MockOrderer) Close() {
	logger.Infof("Closing channels on mock orderer")
	close(o.stop)
}

func cutBlocks(inEnvs <-chan *common.Envelope, blockSize uint64, timeout time.Duration) <-chan *common.Block {
	logger.Debugf("Start cutting blocks")
	outBlocks := make(chan *common.Block, capacity)
	prevBlock := &common.Block{
		Header: &common.BlockHeader{
			Number: 0,
		},
		Data: &common.BlockData{Data: [][]byte{}},
	}
	outBlocks <- prevBlock
	logger.Debugf("Cut debug block")
	go func() {
		for blockNum := uint64(1); true; blockNum++ {
			block := &common.Block{
				Header: &common.BlockHeader{
					Number:       blockNum,
					PreviousHash: protoutil.BlockHeaderHash(prevBlock.Header),
				},
				Data: &common.BlockData{
					Data: make([][]byte, blockSize),
				},
			}
			for txNum := uint64(0); txNum < blockSize; txNum++ {
				env, ok := <-inEnvs
				if !ok {
					logger.Infof("Closing block cutter")
					return
				}
				block.Data.Data[txNum], _ = proto.Marshal(env)
			}
			logger.Debugf("Cut block %d", blockNum)
			outBlocks <- block
			prevBlock = block
		}
	}()
	return outBlocks
}

// Broadcast receives TXs and returns ACKs
func (o *MockOrderer) Broadcast(stream ab.AtomicBroadcast_BroadcastServer) error {
	addr := util.ExtractRemoteAddress(stream.Context())
	logger.Infof("Starting broadcast with %s", addr)

	for {
		select {
		case <-o.stop:
			logger.Infof("Stopping broadcast to %s", addr)
			return nil
		default:
		}

		env, err := stream.Recv()
		if err == io.EOF {
			logger.Warnf("Received EOF from %v, hangup\n", stream)
			return nil
		}
		if err != nil {
			logger.Errorf("Error reading from %v: %s\n", stream, err)
			return err
		}
		o.inEnvs <- env

		err = stream.Send(&ab.BroadcastResponse{
			Status: common.Status_SUCCESS,
		})
		if err != nil {
			return errors.Wrap(err, "error sending ack")
		}
	}
}

// Deliver receives a seek request and returns a stream of the orderered blocks
func (o *MockOrderer) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	addr := util.ExtractRemoteAddress(stream.Context())
	logger.Infof("Starting delivery with %s", addr)

	seekInfo, channelID, err := readSeekEnvelope(stream)
	if err != nil {
		return errors.Wrap(err, "failed reading seek request")
	}
	logger.Infof("Received listening request for channel '%s': %v\nWe will ignore the request and send a stream anyway.", channelID, *seekInfo)

	for {
		select {
		case <-o.stop:
			logger.Infof("Stopping delivery to %s", addr)
			return nil
		case block := <-o.outBlocks:
			if err := stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{Block: block}}); err != nil {
				return errors.Wrap(err, "failed sending block")
			}
			logger.Debugf("Emitted block %d", block.Header.Number)
		}
	}
}

func readSeekEnvelope(stream ab.AtomicBroadcast_DeliverServer) (*ab.SeekInfo, string, error) {
	env, err := stream.Recv()
	if err != nil {
		return nil, "", err
	}
	payload, err := protoutil.UnmarshalPayload(env.Payload)
	if err != nil {
		return nil, "", err
	}
	seekInfo := &ab.SeekInfo{}
	if err = proto.Unmarshal(payload.Data, seekInfo); err != nil {
		return nil, "", err
	}

	chdr := &common.ChannelHeader{}
	if err = proto.Unmarshal(payload.Header.ChannelHeader, chdr); err != nil {
		return nil, "", err
	}
	return seekInfo, chdr.ChannelId, nil
}

func StartMockOrderingService(numService int) ([]*connection.ServerConfig, []*MockOrderer, []*grpc.Server) {
	os := make([]*MockOrderer, numService)

	inEnvsPerOrderer := make([]<-chan *common.Envelope, numService)
	outBlocksPerOrderer := make([]chan<- *common.Block, numService)
	for i := 0; i < numService; i++ {
		outBlocks := make(chan *common.Block, capacity)
		inEnvs := make(chan *common.Envelope, capacity)
		outBlocksPerOrderer[i] = outBlocks
		inEnvsPerOrderer[i] = inEnvs
		os[i] = NewSharedMockOrderer(inEnvs, outBlocks)
	}

	allInEnvs := fanIn(inEnvsPerOrderer)
	allOutBlocks := cutBlocks(allInEnvs, 100, 10*time.Second)
	fanOut(allOutBlocks, outBlocksPerOrderer)

	sc, grpcSrvs := test.StartMockServers(numService, func(server *grpc.Server, index int) {
		ab.RegisterAtomicBroadcastServer(server, os[index])
	})
	return sc, os, grpcSrvs
}

func fanOut(blocks <-chan *common.Block, chans []chan<- *common.Block) {
	go func() {
		for block := range blocks {
			logger.Debugf("Block %d, prev: [%x], current: [%x]", block.Header.Number, block.Header.PreviousHash, block.Header.DataHash)
			for _, ch := range chans {
				ch <- block
			}
		}
	}()
}

func fanIn(chans []<-chan *common.Envelope) <-chan *common.Envelope {
	return chans[0]
	//TODO: AF
	//result := make(chan *common.Envelope, 100)
	//for _, ch := range chans {
	//	go func(ch <-chan *common.Envelope) {
	//		for env := range ch {
	//			result <- env
	//		}
	//	}(ch)
	//}
	//return result
}
