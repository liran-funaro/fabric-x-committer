package sidecar

import (
	"fmt"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("sidecar")

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
	RunCommittedBlockListener(onFullBlockStatusComplete func(*common.Block))
}

type Sidecar struct {
	ordererListener      OrdererListener
	committerAdapter     CommitterAdapter
	orderedBlocks        chan *workload.BlockWithExpectedResult
	postCommitAggregator PostCommitAggregator
}

type InitOptions struct {
	ChannelID                      string
	CommitterEndpoint              connection.Endpoint
	CommitterOutputChannelCapacity int
	OrdererTransportCredentials    credentials.TransportCredentials
	OrdererSigner                  msp.SigningIdentity
	OrdererEndpoint                connection.Endpoint
}

func New(orderer *OrdererClientConfig, committer *CommitterClientConfig, security *clients.SecurityConnectionOpts) (*Sidecar, error) {
	logger.Infof("Initializing sidecar:\n"+
		"\tOrderer:\n"+
		"\t\tEndpoint: %v\n"+
		"\t\tChannel: '%s'\n"+
		"\tCommitter:\n"+
		"\t\tEndpoint: %v\n"+
		"\t\tOutput channel capacity: %d\n", orderer.Endpoint, orderer.ChannelID, committer.Endpoint, committer.OutputChannelCapacity)
	ordererListener, err := clients.NewFabricOrdererListener(&clients.FabricOrdererConnectionOpts{
		ChannelID:              orderer.ChannelID,
		Endpoint:               orderer.Endpoint,
		SecurityConnectionOpts: security,
	})
	if err != nil {
		return nil, err
	}

	return &Sidecar{
		ordererListener:      ordererListener,
		committerAdapter:     client.OpenCoordinatorAdapter(committer.Endpoint),
		orderedBlocks:        make(chan *workload.BlockWithExpectedResult, committer.OutputChannelCapacity),
		postCommitAggregator: NewTxStatusAggregator(),
	}, nil
}

func (s *Sidecar) Start(onBlockCommitted func(*common.Block)) {
	logger.Infof("Starting up sidecar\n")
	go func() {
		utils.Must(s.ordererListener.RunOrdererOutputListener(func(msg *ab.DeliverResponse) {
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
		}))
	}()

	go func() {
		s.committerAdapter.RunCommitterSubmitterListener(s.orderedBlocks, func(t time.Time, b *token.Block) {
			fmt.Printf("Received block from orderer: %d:%d\n", b.Number, len(b.Txs))
		}, s.postCommitAggregator.AddCommittedBatch)
	}()

	s.postCommitAggregator.RunCommittedBlockListener(onBlockCommitted)
}

//TODO: Implement mapping
func mapBlock(block *common.Block) *token.Block {
	return &token.Block{
		Number: block.Header.Number,
		Txs:    make([]*token.Tx, len(block.Data.Data)),
	}
}

func isConfigBlock(block *common.Block) bool {
	return false
}
