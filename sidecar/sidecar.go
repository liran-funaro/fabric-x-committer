package sidecar

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"google.golang.org/grpc/credentials"
)

type CommitterAdapter interface {
	RunCommitterSubmitterListener(
		blocks chan *workload.BlockWithExpectedResult, // TODO: Change interface to accept token.Block
		onSubmit func(time.Time, *token.Block),
		onReceive func(*coordinatorservice.TxValidationStatusBatch))
}
type OrdererListener interface {
	RunOrdererOutputListener(onOrderedBlockReceive func(*ab.DeliverResponse)) error
}
type PostCommitAggregator interface {
	AddSubmittedConfigBlock(block *common.Block)
	AddSubmittedTxBlock(*common.Block)
	AddCommittedBatch(*coordinatorservice.TxValidationStatusBatch)
	RunCommittedBlockListener(onFullBlockStatusComplete func(*ab.DeliverResponse_Block))
}
type OutputBroadcaster interface {
	Broadcast(*ab.DeliverResponse_Block)
}

type Sidecar struct {
	ordererListener         OrdererListener
	committerAdapter        CommitterAdapter
	orderedBlocks           chan *workload.BlockWithExpectedResult
	postCommitAggregator    PostCommitAggregator
	postCommitAggregatorOut chan *ab.DeliverResponse_Block
	outputBroadcaster       OutputBroadcaster
}

type InitOptions struct {
	ChannelID                   string
	CommitterEndpoint           connection.Endpoint
	OrdererTransportCredentials credentials.TransportCredentials
	OrdererEndpoint             connection.Endpoint
	OutputEndpoint              connection.Endpoint
}

func New(opts *InitOptions) (*Sidecar, error) {
	return nil, nil
}

func (s *Sidecar) Start() {

	go func() {
		s.ordererListener.RunOrdererOutputListener(func(msg *ab.DeliverResponse) {
			if t, ok := msg.Type.(*ab.DeliverResponse_Block); ok {
				if isConfigBlock(t.Block) {
					s.postCommitAggregator.AddSubmittedConfigBlock(t.Block)
				} else {
					s.postCommitAggregator.AddSubmittedTxBlock(t.Block)
					s.orderedBlocks <- &workload.BlockWithExpectedResult{
						Block: mapBlock(t.Block),
					}
				}
			}
		})
	}()

	go func() {
		s.committerAdapter.RunCommitterSubmitterListener(s.orderedBlocks, func(time.Time, *token.Block) {}, s.postCommitAggregator.AddCommittedBatch)
	}()

	s.postCommitAggregator.RunCommittedBlockListener(s.outputBroadcaster.Broadcast)
}

func mapBlock(block *common.Block) *token.Block {
	return nil
}

func isConfigBlock(block *common.Block) bool {
	return false
}
