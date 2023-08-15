package sidecar

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload/client"
	"google.golang.org/grpc/credentials"
)

var logger = logging.New("sidecar")

// DeliverListener connects to the orderer, and only listens for orderered blocks
type DeliverListener interface {
	RunDeliverOutputListener(onOrderedBlockReceive func(block *common.Block)) error
}

// CommitterSubmitterListener connects to the committer (i.e. the coordinator of the committer), and submits blocks and listens for status batches.
type CommitterSubmitterListener interface {
	//RunCommitterSubmitterListener commits blocks to the committer
	RunCommitterSubmitterListener(
		blocks <-chan *workload.BlockWithExpectedResult, // TODO: Change interface to accept token.Block
		onSubmit func(time.Time, *token.Block),
		onReceive func(*coordinatorservice.TxValidationStatusBatch))
}

// PostCommitAggregator is an adapter between the scalable committer and the client
// SC returns the status of a committed TX as fast as possible, without waiting for the rest of the TXs of the same block.
// However, the client is listening for whole blocks.
// This component collects the sharded TX statuses and aggregates them until it collects the entire block.
// Then the entire block is output in the correct order (as defined by the orderer).
type PostCommitAggregator interface {
	//AddSubmittedBlock adds a TX block to the aggregator and keeps a list of the (non-config, non-issue) TXs that have not been validated by the committer.
	//Once we collect the statuses of all TXs from the committer, the block is marked as complete and can be output (after all previous blocks have been output).
	AddSubmittedBlock(block *common.Block, excluded []int)
	//AddCommittedBatch registers to the aggregator a batch of (in)validated TXs that came from the committer.
	//A batch contains TXs belonging to different blocks.
	//Once we collect all TXs that belong to a specific block, that block is marked as complete.
	AddCommittedBatch(*coordinatorservice.TxValidationStatusBatch)
	//RunCommittedBlockListener listens for the complete blocks in the output in the correct order (as defined by the orderer).
	RunCommittedBlockListener(onFullBlockStatusComplete func(*common.Block))
}

type Sidecar struct {
	ordererListener      DeliverListener
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
	startBlock := 0
	logger.Infof("Initializing sidecar:\n"+
		"\tOrderer:\n"+
		"\t\tEndpoint: %v\n"+
		"\t\tChannel: '%s'\n"+
		"\t\tReconnect: %v\n"+
		"\tCommitter:\n"+
		"\t\tEndpoint: %v\n"+
		"\t\tOutput channel capacity: %d\n", orderer.Endpoint, orderer.ChannelID, orderer.Reconnect, committer.Endpoint, committer.OutputChannelCapacity)
	ordererListener, err := deliver.NewListener(&deliver.ConnectionOpts{
		ClientProvider: &OrdererDeliverClientProvider{},
		ChannelID:      orderer.ChannelID,
		Endpoint:       orderer.Endpoint,
		Credentials:    creds,
		Signer:         signer,
		Reconnect:      orderer.Reconnect,
		StartBlock:     int64(startBlock),
	})
	if err != nil {
		return nil, err
	}

	metrics.OrdereredBlocksChLength.SetCapacity(committer.OutputChannelCapacity)

	return &Sidecar{
		ordererListener:      ordererListener,
		committerAdapter:     client.OpenCoordinatorAdapter(committer.Endpoint, nil),
		orderedBlocks:        make(chan *workload.BlockWithExpectedResult, committer.OutputChannelCapacity),
		postCommitAggregator: NewTxStatusAggregator(uint64(startBlock)),
		metrics:              metrics,
	}, nil
}

func (s *Sidecar) Start(onBlockCommitted func(*common.Block)) {
	logger.Infof("Starting up sidecar\n")
	go func() {
		utils.Must(s.ordererListener.RunDeliverOutputListener(func(b *common.Block) {
			block, excluded := mapBlock(b)
			if s.metrics.Enabled {
				//for txNum := uint64(0); txNum < uint64(len(block.Txs)); txNum++ {
				//	s.metrics.RequestTracer.Start(token.TxSeqNum{block.Number, txNum})
				//}
				for txNum := uint64(0); txNum < uint64(len(b.Data.Data)); txNum++ {
					s.metrics.RequestTracer.Start(token.TxSeqNum{b.Header.Number, txNum})
				}
				s.metrics.OrdereredBlocksChLength.Set(len(s.orderedBlocks))
				s.metrics.InTxs.Add(len(b.Data.Data))
			}
			s.postCommitAggregator.AddSubmittedBlock(b, excluded)
			if len(block.Txs) > 0 {
				s.orderedBlocks <- &workload.BlockWithExpectedResult{
					Block: block,
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
			//for txNum, statusCode := range block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER] {
			//	if _, channelHeader, err := serialization.UnwrapEnvelope(block.Data.Data[txNum]); err == nil {
			//		// TODO: AF Will not work if we mix config or issue TXs with transfer TXs. Config TXs are never mixed. Issue TXs are not mixed for now, and they will be later sent to the committer.
			//		s.metrics.RequestTracer.End(token.TxSeqNum{block.Header.Number, uint64(txNum)},
			//			attribute.String(metrics.StatusLabel, StatusInverseMap[statusCode].String()),
			//			attribute.String(metrics.TxIdLabel, channelHeader.TxId))
			//	}
			//}
			for txNum := uint64(0); txNum < uint64(len(block.Data.Data)); txNum++ {
				s.metrics.RequestTracer.End(token.TxSeqNum{block.Header.Number, txNum})
			}
			s.metrics.OutTxs.Add(len(block.Data.Data))
		}
		logger.Infof("Received complete block from committer: %d:%d.", block.Header.Number, len(block.Data.Data))
		onBlockCommitted(block)
	})
}

func mapBlock(block *common.Block) (*token.Block, []int) {
	// A config block contains only a single transaction
	if len(block.Data.Data) == 1 && serialization.IsConfigTx(block.Data.Data[0]) {
		return &token.Block{Number: block.Header.Number}, []int{0}
	}

	excluded := make([]int, 0, len(block.Data.Data))
	txs := make([]*token.Tx, 0, len(block.Data.Data))
	for i, msg := range block.Data.Data {
		if data, _, err := serialization.UnwrapEnvelope(msg); err != nil {
			logger.Infof("error occurred: %v", err)
			panic(err)
		} else if tx := serialization.UnmarshalTx(data); isIssueTx(tx) {
			excluded = append(excluded, i)
		} else {
			txs = append(txs, tx)
		}
	}
	return &token.Block{
		Number: block.Header.Number,
		Txs:    txs,
	}, excluded
}

func isConfigHeader(channelHeader *common.ChannelHeader) bool {
	return channelHeader.Type == int32(common.HeaderType_CONFIG)
}

func isIssueTx(tx *token.Tx) bool {
	return len(tx.SerialNumbers) == 0
}
