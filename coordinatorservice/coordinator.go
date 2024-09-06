package coordinatorservice

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

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

		// uncommittedTxIDs is used to store the transaction IDs of uncommitted transactions. This is used to
		// detect duplicate transaction IDs among active transactions. While the vcservice detects duplicate txIDs
		// among committed and to-be-committed transactions, we need to identify duplicate txIDs among active
		// transactions to avoid sending duplicate transactions to the vcservice. This is because it is possible to
		// send two transactions with the same txID to either the same or different vcservice nodes.
		// While vcservice ensures that only one of the transactions is committed, it is possible to commit them in
		// an incorrect order.
		// For example, let's assume T1 and T2 have the same txID, and T1 occurred before T2 in  a block. Furthermore,
		// assume that this txID is not already committed. It is possible that one organization might commit T1 and
		// abort T2 while another might commit T2 and abort T1. This could result in incorrect results and forks among
		// network nodes. Hence, we need to identify duplicate txIDs among active transactions and reject them early.
		// Note that the vcservice will still detect duplicate txIDs among committed and to-be-committed transactions
		// to ensure that no duplicate transactions are committed. Note that we are not directly limiting the number
		// of entries in uncommittedTxIDs as it is controlled indirectly by the size of various channels and gRPC
		// flow control.
		uncommittedTxIDs *sync.Map

		uncommittedMetaNsTx *sync.Map

		// sendStreamMu is used to synchronize sending transaction status over grpc stream
		// to the client between the goroutine that process status from sigverifier and the goroutine
		// that process status from validator.
		sendTxStreamMu sync.Mutex

		// ctx is a derived context, linked to its parent, that specifically controls the cancellation
		// of all goroutines within the coordinator service.
		ctx context.Context
		// cancel is a function to manually trigger the cancellation of this context (and its children).
		cancel context.CancelFunc
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
		// receiver: coordinator receives transaction status from this channel and forward them to sidecar.
		txsStatus chan *protovcservice.TransactionStatus
	}
)

// NewCoordinatorService creates a new coordinator service.
func NewCoordinatorService(ctx context.Context, c *CoordinatorConfig) *CoordinatorService {
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
	}

	metrics := newPerformanceMetrics(c.Monitoring.Metrics.Enable)
	var promErrChan <-chan error
	if metrics.enabled {
		promErrChan = metrics.provider.StartPrometheusServer(c.Monitoring.Metrics.Endpoint)
	}

	ctx, cancel := context.WithCancel(ctx)

	svMgr := newSignatureVerifierManager(
		ctx,
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
		ctx,
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
		promErrChan:                      promErrChan,
		uncommittedTxIDs:                 &sync.Map{},
		uncommittedMetaNsTx:              &sync.Map{},
		ctx:                              ctx,
		cancel:                           cancel,
	}
}

// Start starts each manager in the coordinator service.
func (c *CoordinatorService) Start() (chan error, error) {
	go c.monitorQueues()

	logger.Info("Starting signature verifier manager")
	if err := c.signatureVerifierMgr.start(); err != nil {
		return nil, err
	}

	logger.Info("Starting dependency graph manager")
	c.dependencyMgr.Start()

	logger.Info("Starting validator committer manager")
	valCommitErrChan, err := c.validatorCommitterMgr.start()
	if err != nil {
		return nil, err
	}

	logger.Info("Coordinator service started successfully")

	logger.Info("Started a goroutine to receive block with valid transactions and" +
		" forward it to the dependency graph manager")
	go c.receiveFromSignatureVerifierAndForwardToDepGraph()

	logger.Info("Started a goroutine to receive block with invalid transactions from signature verifier and" +
		" forward the status to validator-committer manager")
	go c.receiveFromSignatureVerifierAndForwardToValidatorCommitter()

	return valCommitErrChan, nil
}

// SetMetaNamespaceVerificationKey sets the verification key for the signature verifier manager.
func (c *CoordinatorService) SetMetaNamespaceVerificationKey(
	_ context.Context,
	k *protosigverifierservice.Key,
) (*protocoordinatorservice.Empty, error) {
	if k.NsId != uint32(types.MetaNamespaceID) {
		return nil, errors.New("namespace ID is not meta namespace ID")
	}
	return &protocoordinatorservice.Empty{}, c.signatureVerifierMgr.setVerificationKey(k)
}

// SetLastCommittedBlockNumber set the last committed block number in the database/ledger through a vcservice.
func (c *CoordinatorService) SetLastCommittedBlockNumber(
	ctx context.Context,
	lastBlock *protoblocktx.LastCommittedBlock,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, c.validatorCommitterMgr.setLastCommittedBlockNumber(ctx, lastBlock)
}

// GetLastCommittedBlockNumber get the last committed block number in the database/ledger.
func (c *CoordinatorService) GetLastCommittedBlockNumber(
	ctx context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.LastCommittedBlock, error) {
	return c.validatorCommitterMgr.getLastCommittedBlockNumber(ctx)
}

// BlockProcessing receives a stream of blocks from the client and processes them.
func (c *CoordinatorService) BlockProcessing(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	logger.Info("Start validate and commit stream")

	numErrableGoroutines := 2
	errorChannel := make(chan error, numErrableGoroutines)

	// NOTE: The below two goroutines are tightly coupled to the lifecycle of the stream.
	//       They terminate when the stream ends. Other goroutines within the coordinator
	//       service can safely continue operating to process existing blocks and enqueue
	//       transaction statuses, even after the stream concludes.

	go func() {
		logger.Info("Started a goroutine to receive block and forward it to the signature verifier manager")
		errorChannel <- c.receiveAndProcessBlock(stream)
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

func (c *CoordinatorService) receiveAndProcessBlock( // nolint:gocognit
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	for {
		blk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		// NOTE: Block processing is decoupled from the stream's lifecycle.
		// Even if the stream terminates unexpectedly, any received blocks will
		// still be forwarded to downstream components for complete processing.

		logger.Debugf("Coordinator received block [%d] with %d TXs", blk.Number, len(blk.Txs))

		c.metrics.transactionReceived(len(blk.Txs))

		dupTxs, malformedTxs := c.preProcessBlock(blk)
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

		if len(dupTxs) == 0 {
			continue
		}

		// NOTE: If the stream closes unexpectedly before a transaction status is
		//       delivered to the client, the status is NOT re-queued. Clients MUST
		//       implement robust recovery mechanisms to retrieve lost transaction
		//       statuses directly from the ledger.
		if err := c.sendTxsStatus(stream, dupTxs); err != nil {
			return err
		}
	}
}

func (c *CoordinatorService) receiveFromSignatureVerifierAndForwardToDepGraph() { // nolint:gocognit
	// Signature verifier can send blocks out of order. Hence, we need to keep track of the next block number
	// that we are expecting. If we receive a block with a different block number, we store it in a map.
	// If the map size reaches a limit, we stop sending blocks to the signature verifier manager till we
	// receive the block that we are expecting.
	nextBlockNumber := uint64(0)
	txBatchID := uint64(1)
	outOfOrderBlock := make(map[uint64]*protoblocktx.Block)
	outOfOrderBlockLimit := 500 // TODO: make it configurable

	txsBatchForDependencyGraph := channel.NewWriter(c.ctx, c.queues.txsBatchForDependencyGraph)
	sendBlock := func(block *protoblocktx.Block) (contextAlive bool) {
		if len(block.Txs) == 0 {
			return true
		}

		if ctxAlive := txsBatchForDependencyGraph.Write(
			&dependencygraph.TransactionBatch{
				ID:  txBatchID,
				Txs: block.Txs,
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

	blockWithValidSignTxs := channel.NewReader(c.ctx, c.queues.blockWithValidSignTxs)
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

func (c *CoordinatorService) sendTxStatusFromValidatorCommitter( // nolint:gocognit
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	txsStatus := channel.NewReader(stream.Context(), c.queues.txsStatus)
	for {
		txStatus, ctxAlive := txsStatus.Read()
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

			if err := c.postProcessing(id, status); err != nil {
				return err
			}
		}

		if err := c.sendTxsStatus(stream, valStatus); err != nil {
			return err
		}
	}
}

func (c *CoordinatorService) receiveFromSignatureVerifierAndForwardToValidatorCommitter() {
	blockWithInvalidSignTxs := channel.NewReader(c.ctx, c.queues.blockWithInvalidSignTxs)
	preliminaryInvalidTxsStatus := channel.NewWriter(c.ctx, c.queues.preliminaryInvalidTxsStatus)
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

func (c *CoordinatorService) postProcessing(txID string, status protoblocktx.Status) error {
	c.uncommittedTxIDs.Delete(txID)

	txNs, ok := c.uncommittedMetaNsTx.Load(txID)
	if !ok {
		return nil
	}
	c.uncommittedMetaNsTx.Delete(txID)
	if status != protoblocktx.Status_COMMITTED {
		return nil
	}

	ns, _ := txNs.(*protoblocktx.TxNamespace) //nolint:revive

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

func (c *CoordinatorService) monitorQueues() {
	// TODO: make sampling time configurable
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-c.ctx.Done():
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

// Close closes each manager in the coordinator service and all channels.
func (c *CoordinatorService) Close() error {
	logger.Infof("Closing all connections to managers")
	c.cancel()
	<-c.ctx.Done()
	c.dependencyMgr.Close()

	close(c.queues.blockForSignatureVerification)
	close(c.queues.blockWithValidSignTxs)
	close(c.queues.blockWithInvalidSignTxs)
	close(c.queues.txsBatchForDependencyGraph)
	close(c.queues.dependencyFreeTxsNode)
	close(c.queues.validatedTxsNode)
	close(c.queues.txsStatus)

	if err := c.metrics.provider.StopServer(); err != nil {
		return err
	}
	return <-c.promErrChan
}
