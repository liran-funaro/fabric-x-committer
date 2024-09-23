package coordinatorservice

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
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
		metrics                          *perfMetrics
		promErrChan                      <-chan error

		uncommittedMetaNsTx *sync.Map

		// sendStreamMu is used to synchronize sending transaction status over grpc stream
		// to the client between the goroutine that process status from sigverifier and the goroutine
		// that process status from validator.
		sendTxStreamMu sync.Mutex
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

		// sender: coordinator sends invalid transactions (due to incorrect formation) to this channel.
		// receiver: validator committer manager receives these invalid transactions from this channel
		//           and forwards them to vcservice for the commit of the invalid status code.
		preliminaryInvalidTxsStatus chan []*protovcservice.Transaction

		// sender: validator committer manager sends validated transactions nodes to this channel. For each validator
		// 	       committer server, there is a goroutine that sends validated transactions nodes to this channel.
		// receiver: dependency graph manager receives validated transactions nodes from this channel and update
		//           the dependency graph.
		validatedTxsNode chan []*dependencygraph.TransactionNode

		// sender: validator committer manager sends transaction status to this channel. For each validator committer
		// 	       server, there is a goroutine that sends transaction status to this channel.
		// receiver: coordinator receives transaction status from this channel and post process them.
		txsStatus chan *protovcservice.TransactionStatus

		// sender: coordinator forwards all post processed txsStatus to this channel.
		// receiver: coordinator receives and forwards them to the sidecar.
		postProcessedTxStatus chan *protovcservice.TransactionStatus
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
		blockForSignatureVerification: make(chan *protoblocktx.Block, bufSzPerChanForSignVerifierMgr),
		blockWithValidSignTxs:         make(chan *protoblocktx.Block, bufSzPerChanForSignVerifierMgr),
		blockWithInvalidSignTxs:       make(chan *protoblocktx.Block, bufSzPerChanForSignVerifierMgr),
		txsBatchForDependencyGraph:    make(chan *dependencygraph.TransactionBatch, bufSzPerChanForLocalDepMgr),
		dependencyFreeTxsNode:         make(chan []*dependencygraph.TransactionNode, bufSzPerChanForValCommitMgr),
		preliminaryInvalidTxsStatus:   make(chan []*protovcservice.Transaction, bufSzPerChanForValCommitMgr),
		validatedTxsNode:              make(chan []*dependencygraph.TransactionNode, bufSzPerChanForValCommitMgr),
		txsStatus:                     make(chan *protovcservice.TransactionStatus, bufSzPerChanForValCommitMgr),
		postProcessedTxStatus:         make(chan *protovcservice.TransactionStatus, bufSzPerChanForValCommitMgr),
	}

	metrics := newPerformanceMetrics(c.Monitoring.Metrics.Enable)

	svMgr := newSignatureVerifierManager(
		&signVerifierManagerConfig{
			serversConfig:                         c.SignVerifierConfig.ServerConfig,
			incomingBlockForSignatureVerification: queues.blockForSignatureVerification,
			outgoingBlockWithValidTxs:             queues.blockWithValidSignTxs,
			outgoingBlockWithInvalidTxs:           queues.blockWithInvalidSignTxs,
			metrics:                               metrics,
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
			WaitingTxsLimit:           c.DependencyGraphConfig.WaitingTxsLimit,
			PrometheusMetricsProvider: metrics.provider,
			MetricsEnabled:            metrics.enabled,
		},
	)

	vcMgr := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			serversConfig:                           c.ValidatorCommitterConfig.ServerConfig,
			incomingTxsForValidationCommit:          queues.dependencyFreeTxsNode,
			incomingPrelimInvalidTxsStatusForCommit: queues.preliminaryInvalidTxsStatus,
			outgoingValidatedTxsNode:                queues.validatedTxsNode,
			outgoingTxsStatus:                       queues.txsStatus,
			metrics:                                 metrics,
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
		metrics:                          metrics,
		uncommittedMetaNsTx:              &sync.Map{},
	}
}

// Run starts each manager in the coordinator service.
func (c *CoordinatorService) Run(ctx context.Context) error {
	g, eCtx := errgroup.WithContext(ctx)

	c.promErrChan = c.metrics.provider.StartPrometheusServer(c.config.Monitoring.Metrics.Endpoint)
	g.Go(func() error {
		c.monitorQueues(eCtx)
		return nil
	})

	g.Go(func() error {
		logger.Info("Starting signature verifier manager")
		return c.signatureVerifierMgr.run(eCtx)
	})

	// TODO: make dependency graph manager use context.
	logger.Info("Starting dependency graph manager")
	c.dependencyMgr.Start()

	g.Go(func() error {
		logger.Info("Starting validator committer manager")
		return c.validatorCommitterMgr.run(eCtx)
	})

	g.Go(func() error {
		logger.Info("Started a goroutine to receive block with valid transactions and" +
			" forward it to the dependency graph manager")
		c.receiveFromSignatureVerifierAndForwardToDepGraph(eCtx)
		return nil
	})

	g.Go(func() error {
		logger.Info("Started a goroutine to receive block with invalid transactions from signature verifier and" +
			" forward the status to validator-committer manager")
		c.receiveFromSignatureVerifierAndForwardToValidatorCommitter(eCtx)
		return nil
	})

	g.Go(func() error {
		return c.postProcessTransactions(eCtx)
	})

	return g.Wait()
}

// SetMetaNamespaceVerificationKey sets the verification key for the signature verifier manager.
func (c *CoordinatorService) SetMetaNamespaceVerificationKey(
	ctx context.Context,
	k *protosigverifierservice.Key,
) (*protocoordinatorservice.Empty, error) {
	if k.NsId != uint32(types.MetaNamespaceID) {
		return nil, errors.New("namespace ID is not meta namespace ID")
	}
	return &protocoordinatorservice.Empty{}, c.signatureVerifierMgr.setVerificationKey(ctx, k)
}

// SetLastCommittedBlockNumber set the last committed block number in the database/ledger through a vcservice.
func (c *CoordinatorService) SetLastCommittedBlockNumber(
	ctx context.Context,
	lastBlock *protoblocktx.BlockInfo,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, c.validatorCommitterMgr.setLastCommittedBlockNumber(ctx, lastBlock)
}

// GetLastCommittedBlockNumber get the last committed block number in the database/ledger.
func (c *CoordinatorService) GetLastCommittedBlockNumber(
	ctx context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return c.validatorCommitterMgr.getLastCommittedBlockNumber(ctx)
}

// GetMaxSeenBlockNumber get the last committed block number in the database/ledger.
func (c *CoordinatorService) GetMaxSeenBlockNumber(
	ctx context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return c.validatorCommitterMgr.getMaxSeenBlockNumber(ctx)
}

// BlockProcessing receives a stream of blocks from the client and processes them.
func (c *CoordinatorService) BlockProcessing(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	logger.Info("Start validate and commit stream")

	g, eCtx := errgroup.WithContext(stream.Context())

	// NOTE: The below two goroutines are tightly coupled to the lifecycle of the stream.
	//       They terminate when the stream ends. Other goroutines within the coordinator
	//       service can safely continue operating to process existing blocks and enqueue
	//       transaction statuses, even after the stream concludes.

	g.Go(func() error {
		logger.Info("Started a goroutine to receive block and forward it to the signature verifier manager")
		return c.receiveAndProcessBlock(eCtx, stream)
	})

	g.Go(func() error {
		logger.Info("Started a goroutine to receive transaction status from validator committer manager and" +
			" forward the status to client")
		return c.sendTxStatus(eCtx, stream)
	})

	return g.Wait()
}

func (c *CoordinatorService) receiveAndProcessBlock( // nolint:gocognit
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	for ctx.Err() == nil {
		blk, err := stream.Recv()
		if err != nil {
			return err
		}

		// NOTE: Block processing is decoupled from the stream's lifecycle.
		// Even if the stream terminates unexpectedly, any received blocks will
		// still be forwarded to downstream components for complete processing.

		logger.Debugf("Coordinator received block [%d] with %d TXs", blk.Number, len(blk.Txs))

		c.metrics.transactionReceived(len(blk.Txs))

		malformedTxs := c.preProcessBlock(blk)
		if len(malformedTxs) != 0 {
			c.queues.preliminaryInvalidTxsStatus <- malformedTxs
		}

		if len(blk.Txs) == 0 {
			c.queues.blockWithValidSignTxs <- blk
		} else {
			c.orderEnforcer.L.Lock()
			for c.stopSendingBlockToSigVerifierMgr.Load() {
				logger.Warn("Stop sending block to signature verifier manager due to out of order blocks")
				c.orderEnforcer.Wait()
			}
			c.orderEnforcer.L.Unlock()

			c.queues.blockForSignatureVerification <- blk
			logger.Debugf("Block [%d] was pushed to the sv manager for processing", blk.Number)
		}
	}

	return nil
}

func (c *CoordinatorService) receiveFromSignatureVerifierAndForwardToDepGraph(ctx context.Context) { // nolint:gocognit
	// Signature verifier can send blocks out of order. Hence, we need to keep track of the next block number
	// that we are expecting. If we receive a block with a different block number, we store it in a map.
	// If the map size reaches a limit, we stop sending blocks to the signature verifier manager till we
	// receive the block that we are expecting.
	nextBlockNumber := uint64(0)
	txBatchID := uint64(1)
	outOfOrderBlock := make(map[uint64]*protoblocktx.Block)
	outOfOrderBlockLimit := 500 // TODO: make it configurable

	txsBatchForDependencyGraph := channel.NewWriter(ctx, c.queues.txsBatchForDependencyGraph)
	sendBlock := func(block *protoblocktx.Block) (contextAlive bool) {
		if len(block.Txs) == 0 {
			return true
		}

		if ctxAlive := txsBatchForDependencyGraph.Write(
			&dependencygraph.TransactionBatch{
				ID:          txBatchID,
				BlockNumber: block.Number,
				Txs:         block.Txs,
			}); !ctxAlive {
			return false
		}
		txBatchID++
		return true
	}

	sendBlockFromOutOfOrderMap := func() (contextAlive bool) {
		block, ok := outOfOrderBlock[nextBlockNumber]
		for ok {
			logger.Debugf("Block [%d] was out of order and was waiting for previous blocks to arrive. "+
				"It can now be released and deleted from the map with waiting blocks.", block.Number)
			if ctxAlive := sendBlock(block); !ctxAlive {
				return false
			}

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
		logger.Debugf("Block [%d] was not out of order (and hence not in the map with waiting blocks). "+
			"Continuing execution...", nextBlockNumber)
		return true
	}

	blockWithValidSignTxs := channel.NewReader(ctx, c.queues.blockWithValidSignTxs)
	for {
		block, ctxAlive := blockWithValidSignTxs.Read()
		if !ctxAlive {
			return
		}

		logger.Debugf("Block [%d] with %d valid TXs reached the dependency graph", block.Number, len(block.Txs))
		switch block.Number {
		case nextBlockNumber:
			logger.Debugf("Block [%d] is in order", block.Number)
			sendBlock(block)
			nextBlockNumber++
			sendBlockFromOutOfOrderMap()
		default:
			logger.Debugf("Block [%d] is out of order. Will be stored in a map with waiting blocks.", block.Number)
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

func (c *CoordinatorService) sendTxStatus(
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	postProcessedTxsStatus := channel.NewReader(ctx, c.queues.postProcessedTxStatus)
	for {
		txStatus, ctxAlive := postProcessedTxsStatus.Read()
		if !ctxAlive {
			return nil
		}

		logger.Debugf("New batch with %d TX statuses reached the coordinator", len(txStatus.Status))
		valStatus := make([]*protocoordinatorservice.TxValidationStatus, 0, len(txStatus.Status))

		for id, status := range txStatus.Status {
			valStatus = append(valStatus, &protocoordinatorservice.TxValidationStatus{
				TxId:   id,
				Status: status,
			})
		}

		if err := c.sendTxsStatus(stream, valStatus); err != nil {
			return err
		}
	}
}

func (c *CoordinatorService) receiveFromSignatureVerifierAndForwardToValidatorCommitter(ctx context.Context) {
	blockWithInvalidSignTxs := channel.NewReader(ctx, c.queues.blockWithInvalidSignTxs)
	preliminaryInvalidTxsStatus := channel.NewWriter(ctx, c.queues.preliminaryInvalidTxsStatus)
	for {
		blkWithInvalidSign, ctxAlive := blockWithInvalidSignTxs.Read()
		if !ctxAlive {
			return
		}

		invalidTxsStatus := make([]*protovcservice.Transaction, len(blkWithInvalidSign.Txs))
		for i, tx := range blkWithInvalidSign.Txs {
			invalidTxsStatus[i] = &protovcservice.Transaction{
				ID: tx.Id,
				PrelimInvalidTxStatus: &protovcservice.InvalidTxStatus{
					Code: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
				},
				BlockNumber: blkWithInvalidSign.Number,
			}
		}
		// NOTE: we are not sending the invalid tx status immediately to the client as
		// this status code can change if the txID is found to be duplicate by the
		// vcservice.
		preliminaryInvalidTxsStatus.Write(invalidTxsStatus)
	}
}

func (c *CoordinatorService) sendTxsStatus(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
	txsStatus []*protocoordinatorservice.TxValidationStatus,
) error {
	c.sendTxStreamMu.Lock()
	defer c.sendTxStreamMu.Unlock()
	if err := stream.Send(
		&protocoordinatorservice.TxValidationStatusBatch{
			TxsValidationStatus: txsStatus,
		},
	); err != nil {
		return err
	}

	logger.Debugf("Batch with %d TX statuses forwarded to output stream.", len(txsStatus))

	// TODO: introduce metrics to record all sent statuses. Issue #436.
	m := c.metrics
	for _, tx := range txsStatus {
		switch tx.Status {
		case protoblocktx.Status_COMMITTED:
			m.addToCounter(m.transactionCommittedStatusSentTotal, 1)
		case protoblocktx.Status_ABORTED_MVCC_CONFLICT:
			m.addToCounter(m.transactionMVCCConflictStatusSentTotal, 1)
		case protoblocktx.Status_ABORTED_DUPLICATE_TXID:
			m.addToCounter(m.transactionDuplicateTxStatusSentTotal, 1)
		case protoblocktx.Status_ABORTED_SIGNATURE_INVALID:
			m.addToCounter(m.transactionInvalidSignatureStatusSentTotal, 1)
		}
	}

	return nil
}

func (c *CoordinatorService) postProcessTransactions(ctx context.Context) error {
	txsStatus := channel.NewReader(ctx, c.queues.txsStatus)
	postProcessedTxsStatus := channel.NewWriter(ctx, c.queues.postProcessedTxStatus)
	for {
		txs, ok := txsStatus.Read()
		if !ok {
			return nil
		}

		for txID, status := range txs.Status {
			txNs, loaded := c.uncommittedMetaNsTx.LoadAndDelete(txID)
			if !loaded {
				continue
			}
			if status != protoblocktx.Status_COMMITTED {
				continue
			}

			ns, _ := txNs.(*protoblocktx.TxNamespace) //nolint:revive
			if err := c.postProcessMetaNsTx(ctx, ns); err != nil {
				return err
			}
		}
		postProcessedTxsStatus.Write(txs)
	}
}

func (c *CoordinatorService) postProcessMetaNsTx(ctx context.Context, ns *protoblocktx.TxNamespace) error {
	for _, rw := range ns.ReadWrites {
		nsID, _ := types.NamespaceIDFromBytes(rw.Key)
		nsVersion := types.VersionNumber(0).Bytes()

		if rw.Version != nil {
			preVerNo := types.VersionNumberFromBytes(rw.Version)
			nsVersion = (preVerNo + 1).Bytes()
		}

		p := &protoblocktx.NamespacePolicy{}
		_ = proto.Unmarshal(rw.Value, p)

		if err := c.signatureVerifierMgr.setVerificationKey(
			ctx,
			&protosigverifierservice.Key{
				NsId:            uint32(nsID),
				NsVersion:       nsVersion,
				SerializedBytes: p.PublicKey,
				Scheme:          p.Scheme,
			},
		); err != nil {
			return err
		}
	}

	return nil
}

func (c *CoordinatorService) monitorQueues(ctx context.Context) {
	// TODO: make sampling time configurable
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case err := <-c.promErrChan:
			// Once the prometheus server is stopped, we no longer need to monitor the queues.
			ticker.Stop()
			if err != nil {
				logger.Errorf("Prometheus ended with error: %v", err)
			} else {
				logger.Info("Prometheus server has been stopped")
			}
			return
		}

		m := c.metrics
		q := c.queues
		m.setQueueSize(m.sigverifierInputBlockQueueSize, len(q.blockForSignatureVerification))
		m.setQueueSize(m.sigverifierOutputValidBlockQueueSize, len(q.blockWithValidSignTxs))
		m.setQueueSize(m.sigverifierOutputInvalidBlockQueueSize, len(q.blockWithInvalidSignTxs))
		m.setQueueSize(m.vcserviceInputTxBatchQueueSize, len(q.dependencyFreeTxsNode))
		m.setQueueSize(m.vcserviceOutputValidatedTxBatchQueueSize, len(q.validatedTxsNode))
		m.setQueueSize(m.vcserviceOutputTxStatusBatchQueueSize, len(q.txsStatus))
	}
}

// Close closes dependencyMgr and prometheus server.
func (c *CoordinatorService) Close() error {
	logger.Infof("Closing all connections to managers")
	c.dependencyMgr.Close()

	return c.metrics.provider.StopServer()
}
