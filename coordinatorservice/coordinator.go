package coordinatorservice

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

var logger = logging.New("coordinator service")

type (
	// CoordinatorService is responsible for coordinating signature verification, dependency tracking, and
	// validation and commit of each transaction.
	CoordinatorService struct {
		protocoordinatorservice.UnimplementedCoordinatorServer
		signatureVerifierMgr             *signatureVerifierManager
		dependencyMgr                    *dependencygraph.Manager
		validatorCommitterMgr            *validatorCommitterManager
		queues                           *channels
		config                           *CoordinatorConfig
		stopSendingBlockToSigVerifierMgr *atomic.Bool
		orderEnforcer                    *sync.Cond
	}

	channels struct {
		// sender: coordinator sends blocks to this channel.
		// receiver: signature verifier manager receives blocks from this channel and validate signatures.
		//           For each signature verifier server, there is a goroutine that receives blocks from this channel.
		blockForSignatureVerification chan *protoblocktx.Block

		// sender: signature verifier manager sends blocks with valid transactions to this channel. For each signature
		// 	       verifier server, there is a goroutine that sends blocks to this channel.
		// receiver: coordinator receives blocks with valid transactions from this channel.
		blockWithValidSignTxs chan *protoblocktx.Block

		// sender: signature verifier manager sends blocks with invalid transactions to this channel. For each signature
		// 	       verifier server, there is a goroutine that sends blocks to this channel.
		// receiver: coordinator receives blocks with invalid transactions from this channel, processes them, and
		//           send those transactions to sidecar.
		blockWithInvalidSignTxs chan *protoblocktx.Block

		// sender: coordinator sends valid transactions received from signature verifier manager to this channel.
		// receiver: dependency graph manager receives transactions batch from this channel and construct dependency
		//           graph. For each local dependency constructor, there is a goroutine that receives transactions batch
		//           from this channel.
		txsBatchForDependencyGraph chan *dependencygraph.TransactionBatch

		// sender: dependency graph manager sends dependency free transactions nodes to this channel.
		// receiver: validator committer manager receives dependency free transactions nodes from this channel and
		//           validate and commit them using vcservice. For each validator committer server, there is a goroutine
		// 		     that receives dependency free transactions nodes from this channel.
		dependencyFreeTxsNode chan []*dependencygraph.TransactionNode

		// sender: validator committer manager sends validated transactions nodes to this channel. For each validator
		// 	       committer server, there is a goroutine that sends validated transactions nodes to this channel.
		// receiver: dependency graph manager receives validated transactions nodes from this channel and update
		//           the dependency graph.
		validatedTxsNode chan []*dependencygraph.TransactionNode

		// sender: validator committer manager sends transaction status to this channel. For each validator committer
		// 	       server, there is a goroutine that sends transaction status to this channel.
		// receiver: coordinator receives transaction status from this channel and forward them to sidecar.
		txsStatus chan *protovcservice.TransactionStatus
	}
)

// NewCoordinatorService creates a new coordinator service.
func NewCoordinatorService(c *CoordinatorConfig) *CoordinatorService {
	// We need to calculate the buffer size for each channel based on the number of goroutines accessing the channel
	// in each manager. For sign verifier manager and validator committer manager, we have a goroutine per server to
	// read from and write to the channel. Hence, we define a buffer size for each manager by multiplying the number
	// of servers with the buffer size per goroutine. For dependency graph manager, we have a goroutine for each
	// local dependency constructors. Hence, we define a buffer size for dependency graph manager by multiplying the
	// number of local dependency constructors with the buffer size per goroutine. We follow this approach to avoid
	// giving too many configurations to the user as it would add complexity to the user experience.
	bufSzPerChanForSignVerifierMgr := c.ChannelBufferSizePerGoroutine * len(c.SignVerifierConfig.ServerConfig)
	bufSzPerChanForValCommitMgr := c.ChannelBufferSizePerGoroutine * len(c.ValidatorCommitterConfig.ServerConfig)
	bufSzPerChanForLocalDepMgr := c.ChannelBufferSizePerGoroutine * c.DependencyGraphConfig.NumOfLocalDepConstructors

	queues := &channels{
		blockForSignatureVerification: make(
			chan *protoblocktx.Block,
			bufSzPerChanForSignVerifierMgr,
		),
		blockWithValidSignTxs: make(
			chan *protoblocktx.Block,
			bufSzPerChanForSignVerifierMgr,
		),
		blockWithInvalidSignTxs: make(
			chan *protoblocktx.Block,
			bufSzPerChanForSignVerifierMgr,
		),
		txsBatchForDependencyGraph: make(
			chan *dependencygraph.TransactionBatch,
			bufSzPerChanForLocalDepMgr,
		),
		dependencyFreeTxsNode: make(
			chan []*dependencygraph.TransactionNode,
			bufSzPerChanForValCommitMgr,
		),
		validatedTxsNode: make(
			chan []*dependencygraph.TransactionNode,
			bufSzPerChanForValCommitMgr,
		),
		txsStatus: make(
			chan *protovcservice.TransactionStatus,
			bufSzPerChanForValCommitMgr,
		),
	}

	svMgr := newSignatureVerifierManager(
		&signVerifierManagerConfig{
			serversConfig:                         c.SignVerifierConfig.ServerConfig,
			incomingBlockForSignatureVerification: queues.blockForSignatureVerification,
			outgoingBlockWithValidTxs:             queues.blockWithValidSignTxs,
			outgoingBlockWithInvalidTxs:           queues.blockWithInvalidSignTxs,
		},
	)

	depMgr := dependencygraph.NewManager(
		&dependencygraph.Config{
			IncomingTxs:               queues.txsBatchForDependencyGraph,
			OutgoingDepFreeTxsNode:    queues.dependencyFreeTxsNode,
			IncomingValidatedTxsNode:  queues.validatedTxsNode,
			NumOfLocalDepConstructors: c.DependencyGraphConfig.NumOfLocalDepConstructors,
			WorkerPoolConfigForGlobalDepManager: &workerpool.Config{
				Parallelism:     c.DependencyGraphConfig.NumOfWorkersForGlobalDepManager,
				ChannelCapacity: c.DependencyGraphConfig.NumOfWorkersForGlobalDepManager * 2,
			},
			WaitingTxsLimit: c.DependencyGraphConfig.WaitingTxsLimit,
		},
	)

	vcMgr := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			serversConfig:                  c.ValidatorCommitterConfig.ServerConfig,
			incomingTxsForValidationCommit: queues.dependencyFreeTxsNode,
			outgoingValidatedTxsNode:       queues.validatedTxsNode,
			outgoingTxsStatus:              queues.txsStatus,
		},
	)

	return &CoordinatorService{
		UnimplementedCoordinatorServer:   protocoordinatorservice.UnimplementedCoordinatorServer{},
		signatureVerifierMgr:             svMgr,
		dependencyMgr:                    depMgr,
		validatorCommitterMgr:            vcMgr,
		queues:                           queues,
		config:                           c,
		stopSendingBlockToSigVerifierMgr: &atomic.Bool{},
		orderEnforcer:                    sync.NewCond(&sync.Mutex{}),
	}
}

// Start starts each manager in the coordinator service.
func (c *CoordinatorService) Start() (chan error, chan error, error) {
	logger.Info("Starting signature verifier manager")
	sigVerifierErrChan, err := c.signatureVerifierMgr.start()
	if err != nil {
		return nil, nil, err
	}

	logger.Info("Starting dependency graph manager")
	c.dependencyMgr.Start()

	logger.Info("Starting validator committer manager")
	valCommitErrChan, err := c.validatorCommitterMgr.start()
	if err != nil {
		return nil, nil, err
	}

	logger.Info("Coordinator service started successfully")

	return sigVerifierErrChan, valCommitErrChan, nil
}

// SetVerificationKey sets the verification key for the signature verifier manager.
func (c *CoordinatorService) SetVerificationKey(
	_ context.Context,
	k *protosigverifierservice.Key,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, c.signatureVerifierMgr.setVerificationKey(k)
}

// BlockProcessing receives a stream of blocks from the client and processes them.
func (c *CoordinatorService) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {
	logger.Info("Start validate and commit stream")

	numErrableGoroutines := 3
	errorChannel := make(chan error, numErrableGoroutines)

	go func() {
		logger.Info("Started a goroutine to receive block and forward it to the signature verifier manager")
		errorChannel <- c.receiveAndProcessBlock(stream)
	}()

	go func() {
		logger.Info("Started a goroutine to receive block with valid transactions and" +
			" forward it to the dependency graph manager")
		c.receiveFromSignatureVerifierAndForwardToDepGraph()
	}()

	go func() {
		logger.Info("Started a goroutine to receive block with invalid transactions from signature verifier and" +
			" forward the status to client")
		errorChannel <- c.sendTxStatusFromSignatureVerifier(stream)
	}()

	go func() {
		logger.Info("Started a goroutine to receive transaction status from validator committer manager and" +
			" forward the status to client")
		errorChannel <- c.sendTxStatusFromValidatorCommitter(stream)
	}()

	for i := 0; i < numErrableGoroutines; i++ {
		err := <-errorChannel
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *CoordinatorService) receiveAndProcessBlock(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	for {
		block, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		c.orderEnforcer.L.Lock()
		for c.stopSendingBlockToSigVerifierMgr.Load() {
			logger.Warn("Stop sending block to signature verifier manager due to out of order blocks")
			c.orderEnforcer.Wait()
		}
		c.orderEnforcer.L.Unlock()

		c.queues.blockForSignatureVerification <- block
	}
}

func (c *CoordinatorService) receiveFromSignatureVerifierAndForwardToDepGraph() {
	// Signature verifier can send blocks out of order. Hence, we need to keep track of the next block number
	// that we are expecting. If we receive a block with a different block number, we store it in a map.
	// If the map size reaches a limit, we stop sending blocks to the signature verifier manager till we
	// receive the block that we are expecting.
	nextBlockNumber := uint64(0)
	txBatchID := uint64(1)
	outOfOrderBlock := make(map[uint64]*protoblocktx.Block)
	outOfOrderBlockLimit := 500 // TODO: make it configurable

	sendBlock := func(block *protoblocktx.Block) {
		if len(block.Txs) == 0 {
			return
		}

		c.queues.txsBatchForDependencyGraph <- &dependencygraph.TransactionBatch{
			ID:  txBatchID,
			Txs: block.Txs,
		}
		txBatchID++
	}

	sendBlockFromOutOfOrderMap := func() {
		block, ok := outOfOrderBlock[nextBlockNumber]
		for ok {
			sendBlock(block)

			if len(outOfOrderBlock) == outOfOrderBlockLimit {
				logger.Info("Received the block that we were expecting. Resuming sending blocks to signature verifier")
				c.orderEnforcer.L.Lock()
				c.stopSendingBlockToSigVerifierMgr.Store(false)
				c.orderEnforcer.Signal()
				c.orderEnforcer.L.Unlock()
			}
			delete(outOfOrderBlock, nextBlockNumber)

			nextBlockNumber++
			block, ok = outOfOrderBlock[nextBlockNumber]
		}
	}

	for block := range c.queues.blockWithValidSignTxs {
		switch block.Number {
		case nextBlockNumber:
			sendBlock(block)
			nextBlockNumber++
			sendBlockFromOutOfOrderMap()
		default:
			outOfOrderBlock[block.Number] = block
			if len(outOfOrderBlock) == outOfOrderBlockLimit {
				logger.Info("Reached the limit of out of order blocks. Stopping sending blocks to signature verifier")
				c.orderEnforcer.L.Lock()
				c.stopSendingBlockToSigVerifierMgr.Store(true)
				c.orderEnforcer.L.Unlock()
			}
		}
	}
}

func (c *CoordinatorService) sendTxStatusFromValidatorCommitter(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	for txStatus := range c.queues.txsStatus {
		valStatus := make([]*protocoordinatorservice.TxValidationStatus, len(txStatus.Status))
		i := 0
		for id, status := range txStatus.Status {
			valStatus[i] = &protocoordinatorservice.TxValidationStatus{
				TxId:   id,
				Status: status,
			}
			i++
		}

		if err := sendTxsStatus(stream, valStatus); err != nil {
			return err
		}
	}

	return nil
}

func (c *CoordinatorService) sendTxStatusFromSignatureVerifier(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	for blk := range c.queues.blockWithInvalidSignTxs {
		valStatus := make([]*protocoordinatorservice.TxValidationStatus, len(blk.Txs))
		for i, tx := range blk.Txs {
			valStatus[i] = &protocoordinatorservice.TxValidationStatus{
				TxId:   tx.Id,
				Status: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
			}
		}

		if err := sendTxsStatus(stream, valStatus); err != nil {
			return err
		}
	}

	return nil
}

func sendTxsStatus(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
	txsStatus []*protocoordinatorservice.TxValidationStatus,
) error {
	err := stream.Send(
		&protocoordinatorservice.TxValidationStatusBatch{
			TxsValidationStatus: txsStatus,
		},
	)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}

	return nil
}

// Close closes each manager in the coordinator service and all channels.
func (c *CoordinatorService) Close() error {
	if err := c.signatureVerifierMgr.close(); err != nil {
		return err
	}

	c.dependencyMgr.Close()

	if err := c.validatorCommitterMgr.close(); err != nil {
		return err
	}

	close(c.queues.blockForSignatureVerification)
	close(c.queues.blockWithValidSignTxs)
	close(c.queues.blockWithInvalidSignTxs)
	close(c.queues.txsBatchForDependencyGraph)
	close(c.queues.dependencyFreeTxsNode)
	close(c.queues.validatedTxsNode)
	close(c.queues.txsStatus)

	return nil
}
