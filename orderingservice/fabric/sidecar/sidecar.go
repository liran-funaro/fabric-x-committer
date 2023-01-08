package sidecar

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"
)

var logger = logging.New("sidecar")

//OrdererListener connects to the orderer, and only listens for orderered blocks
type OrdererListener interface {
	RunOrdererOutputListener(onOrderedBlockReceive func(*ab.DeliverResponse)) error
}

//CommitterSubmitterListener connects to the committer (i.e. the coordinator of the committer), and submits blocks and listens for status batches.
type CommitterSubmitterListener interface {
	//RunCommitterSubmitterListener commits blocks to the committer
	RunCommitterSubmitterListener(
		blocks chan *workload.BlockWithExpectedResult, // TODO: Change interface to accept token.Block
		onSubmit func(time.Time, *token.Block),
		onReceive func(*coordinatorservice.TxValidationStatusBatch))
}

//PostCommitAggregator is an adapter between the scalable committer and the client
//SC returns the status of a committed TX as fast as possible, without waiting for the rest of the TXs of the same block.
//However, the client is listening for whole blocks.
//This component collects the sharded TX statuses and aggregates them until it collects the entire block.
//Then the entire block is output in the correct order (as defined by the orderer).
type PostCommitAggregator interface {
	//AddSubmittedTxBlock adds a TX block to the aggregator and keeps a list of the TXs that have not been validated by the committer.
	//Once we collect the statuses of all TXs from the committer, the block is marked as complete and can be output (after all previous blocks have been output).
	AddSubmittedTxBlock(*common.Block)
	//AddSubmittedConfigBlock adds a config block to the aggregator, similarly to AddSubmittedTxBlock.
	//Unlike TX blocks, since config blocks do not have to be validated by the committer, we do not aggregate the statuses of their TXs.
	//Hence, they are marked directly as complete.
	AddSubmittedConfigBlock(*common.Block)
	//AddCommittedBatch registers to the aggregator a batch of (in)validated TXs that came from the committer.
	//A batch contains TXs belonging to different blocks.
	//Once we collect all TXs that belong to a specific block, that block is marked as complete.
	AddCommittedBatch(*coordinatorservice.TxValidationStatusBatch)
	//RunCommittedBlockListener listens for the complete blocks in the output in the correct order (as defined by the orderer).
	RunCommittedBlockListener(onFullBlockStatusComplete func(*common.Block))
}

type Sidecar struct {
	ordererListener      OrdererListener
	committerAdapter     CommitterSubmitterListener
	orderedBlocks        chan *workload.BlockWithExpectedResult
	postCommitAggregator PostCommitAggregator
	metrics              *metrics.Metrics
}

type InitOptions struct {
	ChannelID                      string
	CommitterEndpoint              connection.Endpoint
	CommitterOutputChannelCapacity int
	OrdererTransportCredentials    credentials.TransportCredentials
	OrdererSigner                  msp.SigningIdentity
	OrdererEndpoint                connection.Endpoint
}

func New(orderer *OrdererClientConfig, committer *CommitterClientConfig, creds credentials.TransportCredentials, signer msp.SigningIdentity, metrics *metrics.Metrics) (*Sidecar, error) {
	logger.Infof("Initializing sidecar:\n"+
		"\tOrderer:\n"+
		"\t\tEndpoint: %v\n"+
		"\t\tChannel: '%s'\n"+
		"\tCommitter:\n"+
		"\t\tEndpoint: %v\n"+
		"\t\tOutput channel capacity: %d\n", orderer.Endpoint, orderer.ChannelID, committer.Endpoint, committer.OutputChannelCapacity)
	ordererListener, err := NewFabricOrdererListener(&FabricOrdererConnectionOpts{
		ChannelID:   orderer.ChannelID,
		Endpoint:    orderer.Endpoint,
		Credentials: creds,
		Signer:      signer,
	})
	if err != nil {
		return nil, err
	}

	metrics.OrdereredBlocksChLength.SetCapacity(committer.OutputChannelCapacity)

	return &Sidecar{
		ordererListener:      ordererListener,
		committerAdapter:     client.OpenCoordinatorAdapter(committer.Endpoint),
		orderedBlocks:        make(chan *workload.BlockWithExpectedResult, committer.OutputChannelCapacity),
		postCommitAggregator: NewTxStatusAggregator(),
		metrics:              metrics,
	}, nil
}

func (s *Sidecar) Start(onBlockCommitted func(*common.Block)) {
	logger.Infof("Starting up sidecar\n")
	go func() {
		utils.Must(s.ordererListener.RunOrdererOutputListener(func(msg *ab.DeliverResponse) {
			if t, ok := msg.Type.(*ab.DeliverResponse_Block); ok {
				logger.Infof("Received block %d from orderer", t.Block.Header.Number)
				if s.metrics.Enabled {
					s.metrics.OrdereredBlocksChLength.Set(len(s.orderedBlocks))
					//for txNum := uint64(0); txNum < uint64(len(t.Block.Data.Data)); txNum++ {
					//	s.metrics.RequestTracer.Start(token.TxSeqNum{t.Block.Header.Number, txNum})
					//}
					s.metrics.InTxs.Add(len(t.Block.Data.Data))
				}
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
			if s.metrics.Enabled {
				//for txNum := uint64(0); txNum < uint64(len(b.Txs)); txNum++ {
				//	s.metrics.RequestTracer.AddEvent(token.TxSeqNum{b.Number, txNum}, "Sent to committer.")
				//}
				s.metrics.CommitterInTxs.Add(len(b.Txs))
			}
		}, func(batch *coordinatorservice.TxValidationStatusBatch) {
			if s.metrics.Enabled {
				//for _, tx := range batch.TxsValidationStatus {
				//	s.metrics.RequestTracer.AddEvent(token.TxSeqNum{tx.BlockNum, tx.TxNum}, "Received from committer.")
				//}
				s.metrics.CommitterOutTxs.Add(len(batch.TxsValidationStatus))
			}
			s.postCommitAggregator.AddCommittedBatch(batch)
		})
	}()

	s.postCommitAggregator.RunCommittedBlockListener(func(block *common.Block) {
		if s.metrics.Enabled {
			//for txNum := uint64(0); txNum < uint64(len(block.Data.Data)); txNum++ {
			//	s.metrics.RequestTracer.End(token.TxSeqNum{block.Header.Number, txNum})
			//}
			s.metrics.OutTxs.Add(len(block.Data.Data))
		}
		logger.Infof("Received complete block from committer: %d:%d.", block.Header.Number, len(block.Data.Data))
		onBlockCommitted(block)
	})
}

func mapBlock(block *common.Block) *token.Block {
	txs := make([]*token.Tx, len(block.Data.Data))
	for i, msg := range block.Data.Data {
		var tx token.Tx
		utils.Must(proto.Unmarshal(msg, &tx))
		txs[i] = &tx

	}
	return &token.Block{
		Number: block.Header.Number,
		Txs:    txs,
	}
}

//TODO: AF
func isConfigBlock(block *common.Block) bool {
	return block.Header.Number == 0
}
