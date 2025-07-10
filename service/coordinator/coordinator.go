/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package coordinator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"

	"github.com/hyperledger/fabric-x-committer/api/protoblocktx"
	"github.com/hyperledger/fabric-x-committer/api/protocoordinatorservice"
	"github.com/hyperledger/fabric-x-committer/service/coordinator/dependencygraph"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/channel"
	"github.com/hyperledger/fabric-x-committer/utils/logging"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
)

var logger = logging.New("coordinator service")

type (
	// Service is responsible for coordinating signature verification, dependency tracking, and
	// validation and commit of each transaction.
	Service struct {
		protocoordinatorservice.UnimplementedCoordinatorServer
		dependencyMgr         *dependencygraph.Manager
		signatureVerifierMgr  *signatureVerifierManager
		validatorCommitterMgr *validatorCommitterManager
		policyMgr             *policyManager
		queues                *channels
		config                *Config
		metrics               *perfMetrics

		// nextExpectedBlockNumberToBeReceived denotes the next block number that the coordinator
		// expects to receive from the sidecar. This value is determined based on the last committed
		// block number in the ledger. The sidecar queries the coordinator for this value before
		// starting to pull blocks from the ordering service.
		nextExpectedBlockNumberToBeReceived atomic.Uint64

		// initializationDone is used to find out whether the coordinator service has
		// been initialized or not.
		initializationDone *channel.Ready

		// streamActive guards against concurrent streams from the client (sidecar)
		// to the coordinator and prevents conflicting operations while a stream is
		// active. Only one active stream is allowed at a time to ensure reliable
		// delivery of transaction status updates. Currently, requests and their
		// corresponding responses are not associated with specific streams, so
		// multiple concurrent streams could lead to unreliable status delivery.
		streamActive sync.RWMutex

		numWaitingTxsForStatus *atomic.Int32

		txBatchIDToDepGraph uint64
	}

	channels struct {
		// sender: coordinator sends received transactions to this channel.
		// receiver: dependency graph manager receives transactions batch from this channel and construct dependency
		//           graph.
		coordinatorToDepGraphTxs chan *dependencygraph.TransactionBatch

		// sender: dependency graph manager sends dependency free transactions nodes to this channel.
		// receiver: signature verifier manager receives dependency free transactions nodes from this channel.
		depGraphToSigVerifierFreeTxs chan dependencygraph.TxNodeBatch

		// sender: signature verifier manager sends valid transactions to this channel.
		// receiver: validator-committer manager receives valid transactions from this channel.
		sigVerifierToVCServiceValidatedTxs chan dependencygraph.TxNodeBatch

		// sender: validator committer manager sends validated transactions nodes to this channel. For each validator
		// 	       committer server, there is a goroutine that sends validated transactions nodes to this channel.
		// receiver: dependency graph manager receives validated transactions nodes from this channel and update
		//           the dependency graph.
		// TODO: As signValidatedTxsNode and vcServiceToDepGraphValidatedTxs can cause confusion, it would be better to
		//       rename vcServiceToDepGraphValidatedTxs to committedOrAbortedTxsNode.
		vcServiceToDepGraphValidatedTxs chan dependencygraph.TxNodeBatch

		// sender: validator committer manager sends transaction status to this channel. For each validator committer
		// 	       server, there is a goroutine that sends transaction status to this channel.
		// receiver: coordinator receives transaction status from this channel and forwards them to the sidecar.
		vcServiceToCoordinatorTxStatus chan *protoblocktx.TransactionsStatus
	}
)

var (
	// ErrActiveStreamWaitingTransactions is returned when NumberOfWaitingTransactionsForStatus is called
	// while a stream is active. This value cannot be reliably determined in this state.
	ErrActiveStreamWaitingTransactions = errors.New("cannot determine number of waiting transactions for " +
		"status while stream is active")

	// ErrActiveStreamBlockNumber is returned when GetNextExpectedBlockNumber is called while a stream is active.
	// The next expected block number cannot be reliably determined in this state.
	ErrActiveStreamBlockNumber = errors.New("cannot determine next expected block number while stream is active")

	// ErrExistingStreamOrConflictingOp indicates that a stream cannot be created because a stream already exists
	// or a conflicting gRPC API call is being made concurrently.
	ErrExistingStreamOrConflictingOp = errors.New("stream already exists or conflicting operation in progress")
)

// NewCoordinatorService creates a new coordinator service.
func NewCoordinatorService(c *Config) *Service {
	// We need to calculate the buffer size for each channel based on the number of goroutines accessing the channel
	// in each manager. For sign verifier manager and validator committer manager, we have a goroutine per server to
	// read from and write to the channel. Hence, we define a buffer size for each manager by multiplying the number
	// of servers with the buffer size per goroutine. For dependency graph manager, we have a goroutine for each
	// local dependency constructors. Hence, we define a buffer size for dependency graph manager by multiplying the
	// number of local dependency constructors with the buffer size per goroutine. We follow this approach to avoid
	// giving too many configurations to the user as it would add complexity to the user experience.
	bufSzPerChanForSignVerifierMgr := c.ChannelBufferSizePerGoroutine * len(c.VerifierConfig.Endpoints)
	bufSzPerChanForValCommitMgr := c.ChannelBufferSizePerGoroutine * len(c.ValidatorCommitterConfig.Endpoints)
	bufSzPerChanForLocalDepMgr := c.ChannelBufferSizePerGoroutine * c.DependencyGraphConfig.NumOfLocalDepConstructors

	queues := &channels{
		coordinatorToDepGraphTxs:           make(chan *dependencygraph.TransactionBatch, bufSzPerChanForLocalDepMgr),
		depGraphToSigVerifierFreeTxs:       make(chan dependencygraph.TxNodeBatch, bufSzPerChanForValCommitMgr),
		sigVerifierToVCServiceValidatedTxs: make(chan dependencygraph.TxNodeBatch, bufSzPerChanForSignVerifierMgr),
		vcServiceToDepGraphValidatedTxs:    make(chan dependencygraph.TxNodeBatch, bufSzPerChanForValCommitMgr),
		vcServiceToCoordinatorTxStatus:     make(chan *protoblocktx.TransactionsStatus, bufSzPerChanForValCommitMgr),
	}

	metrics := newPerformanceMetrics()

	depMgr := dependencygraph.NewManager(
		&dependencygraph.Config{
			IncomingTxs:               queues.coordinatorToDepGraphTxs,
			OutgoingDepFreeTxsNode:    queues.depGraphToSigVerifierFreeTxs,
			IncomingValidatedTxsNode:  queues.vcServiceToDepGraphValidatedTxs,
			NumOfLocalDepConstructors: c.DependencyGraphConfig.NumOfLocalDepConstructors,
			WaitingTxsLimit:           c.DependencyGraphConfig.WaitingTxsLimit,
			PrometheusMetricsProvider: metrics.Provider,
		},
	)

	policyMgr := newPolicyManager()

	svMgr := newSignatureVerifierManager(
		&signVerifierManagerConfig{
			clientConfig:             &c.VerifierConfig,
			incomingTxsForValidation: queues.depGraphToSigVerifierFreeTxs,
			outgoingValidatedTxs:     queues.sigVerifierToVCServiceValidatedTxs,
			metrics:                  metrics,
			policyManager:            policyMgr,
		},
	)

	vcMgr := newValidatorCommitterManager(
		&validatorCommitterManagerConfig{
			clientConfig:                   &c.ValidatorCommitterConfig,
			incomingTxsForValidationCommit: queues.sigVerifierToVCServiceValidatedTxs,
			outgoingValidatedTxsNode:       queues.vcServiceToDepGraphValidatedTxs,
			outgoingTxsStatus:              queues.vcServiceToCoordinatorTxStatus,
			metrics:                        metrics,
			policyMgr:                      policyMgr,
		},
	)

	return &Service{
		UnimplementedCoordinatorServer: protocoordinatorservice.UnimplementedCoordinatorServer{},
		dependencyMgr:                  depMgr,
		signatureVerifierMgr:           svMgr,
		validatorCommitterMgr:          vcMgr,
		policyMgr:                      policyMgr,
		queues:                         queues,
		config:                         c,
		metrics:                        metrics,
		initializationDone:             channel.NewReady(),
		numWaitingTxsForStatus:         &atomic.Int32{},
		txBatchIDToDepGraph:            1,
	}
}

// Run starts each manager in the coordinator service.
func (c *Service) Run(ctx context.Context) error {
	canCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, eCtx := errgroup.WithContext(canCtx)

	g.Go(func() error {
		_ = c.metrics.StartPrometheusServer(eCtx, c.config.Monitoring.Server, c.monitorQueues)
		// We don't return error here to avoid stopping the service due to monitoring error.
		// But we use the errgroup to ensure the method returns only when the server exits.
		return nil
	})

	g.Go(func() error {
		logger.Info("Starting dependency graph manager")
		c.dependencyMgr.Run(eCtx)
		return nil
	})

	g.Go(func() error {
		logger.Info("Starting signature verifier manager")
		if err := c.signatureVerifierMgr.run(eCtx); err != nil {
			logger.Errorf("coordinator service stops due to an error returned by signature verifier manager: %v", err)
			return err
		}
		return nil
	})

	g.Go(func() error {
		logger.Info("Starting validator committer manager")
		if err := c.validatorCommitterMgr.run(eCtx); err != nil {
			logger.Errorf("coordinator service stopds due to an error returned by validator committer manager: %v", err)
			return err
		}
		return nil
	})

	if !c.validatorCommitterMgr.ready.WaitForReady(eCtx) {
		return g.Wait()
	}

	// We attempt to recover the policy manager and the last committed block number from the state DB.
	if err := c.validatorCommitterMgr.recoverPolicyManagerFromStateDB(ctx); err != nil {
		return err
	}
	lastCommittedBlock, getErr := c.validatorCommitterMgr.getLastCommittedBlockNumber(ctx)
	if getErr != nil {
		return getErr
	}
	if lastCommittedBlock.Block == nil {
		// no block has been committed.
		c.nextExpectedBlockNumberToBeReceived.Store(0)
	} else {
		c.nextExpectedBlockNumberToBeReceived.Store(lastCommittedBlock.Block.Number + 1)
	}

	c.initializationDone.SignalReady()

	return utils.ProcessErr(g.Wait(), "coordinator processing has been stopped due to err")
}

// WaitForReady wait for coordinator to be ready to be exposed as gRPC service.
// If the context ended before the service is ready, returns false.
func (c *Service) WaitForReady(ctx context.Context) bool {
	// NOTE: We set `initializationDone` only if signature verifier manager
	//       and vcservice manager are ready. Hence, we can just wait for
	//       the coordinator initialization.
	return c.initializationDone.WaitForReady(ctx)
}

// GetConfigTransaction get the config transaction from the state DB.
func (c *Service) GetConfigTransaction(
	ctx context.Context, _ *protocoordinatorservice.Empty,
) (*protoblocktx.ConfigTransaction, error) {
	return c.validatorCommitterMgr.getConfigTransaction(ctx)
}

// SetLastCommittedBlockNumber set the last committed block number in the database/ledger through a vcservice.
func (c *Service) SetLastCommittedBlockNumber(
	ctx context.Context,
	lastBlock *protoblocktx.BlockInfo,
) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, c.validatorCommitterMgr.setLastCommittedBlockNumber(ctx, lastBlock)
}

// GetLastCommittedBlockNumber get the last committed block number in the database/ledger.
func (c *Service) GetLastCommittedBlockNumber(
	ctx context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.LastCommittedBlock, error) {
	return c.validatorCommitterMgr.getLastCommittedBlockNumber(ctx)
}

// GetNextExpectedBlockNumber returns the next expected block number to be received by the coordinator.
func (c *Service) GetNextExpectedBlockNumber(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	if !c.streamActive.TryRLock() {
		return nil, ErrActiveStreamBlockNumber
	}
	defer c.streamActive.RUnlock()

	return &protoblocktx.BlockInfo{
		Number: c.nextExpectedBlockNumberToBeReceived.Load(),
	}, nil
}

// GetTransactionsStatus returns the status of given transactions identifiers.
func (c *Service) GetTransactionsStatus(
	ctx context.Context,
	q *protoblocktx.QueryStatus,
) (*protoblocktx.TransactionsStatus, error) {
	return c.validatorCommitterMgr.getTransactionsStatus(ctx, q)
}

// NumberOfWaitingTransactionsForStatus returns the number of transactions waiting to get the final status.
func (c *Service) NumberOfWaitingTransactionsForStatus(
	_ context.Context,
	_ *protocoordinatorservice.Empty,
) (*protocoordinatorservice.WaitingTransactions, error) {
	if !c.streamActive.TryLock() {
		return nil, ErrActiveStreamWaitingTransactions
	}
	defer c.streamActive.Unlock()

	return &protocoordinatorservice.WaitingTransactions{
		Count: c.numWaitingTxsForStatus.Load() - int32(len(c.queues.vcServiceToCoordinatorTxStatus)), //nolint:gosec
	}, nil
}

// BlockProcessing receives a stream of blocks from the client and processes them.
func (c *Service) BlockProcessing(
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	if !c.streamActive.TryLock() {
		return ErrExistingStreamOrConflictingOp
	}
	defer c.streamActive.Unlock()

	logger.Info("Start validate and commit stream")

	g, eCtx := errgroup.WithContext(stream.Context())

	// NOTE: The below two goroutines are tightly coupled to the lifecycle of the stream.
	//       They terminate when the stream ends. Other goroutines within the coordinator
	//       service can safely continue operating to process existing blocks and enqueue
	//       transaction statuses, even after the stream concludes.

	g.Go(func() error {
		logger.Info("Started a goroutine to receive block and forward it to the dependency graph manager")
		if err := c.receiveAndProcessBlock(eCtx, stream); err != nil {
			logger.Warnf("stream to the coordinator is ending with an error: %v", err)
			return err
		}
		return nil
	})

	g.Go(func() error {
		logger.Info("Started a goroutine to receive transaction status from validator committer manager and" +
			" forward the status to client")
		if err := c.sendTxStatus(eCtx, stream); err != nil {
			logger.Warnf("stream to the coordinator is ending with an error: %v", err)
		}
		return nil
	})

	return utils.ProcessErr(g.Wait(), "stream with the sidecar has ended")
}

func (c *Service) receiveAndProcessBlock(
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	txsBatchForDependencyGraph := channel.NewWriter(ctx, c.queues.coordinatorToDepGraphTxs)

	for ctx.Err() == nil {
		blk, err := stream.Recv()
		if err != nil {
			return errors.Wrap(err, "failed to receive blocks from the sidecar")
		}

		// NOTE: Block processing is decoupled from the stream's lifecycle.
		// Even if the stream terminates unexpectedly, any received blocks will
		// still be forwarded to downstream components for complete processing.

		swapped := c.nextExpectedBlockNumberToBeReceived.CompareAndSwap(blk.Number, blk.Number+1)
		if !swapped {
			errMsg := fmt.Sprintf(
				"coordinator expects block [%d] but received block [%d]",
				c.nextExpectedBlockNumberToBeReceived.Load(),
				blk.Number,
			)
			logger.Error(errMsg)
			return errors.New(errMsg)
		}
		logger.Debugf("Coordinator received block [%d] with %d TXs", blk.Number, len(blk.Txs))

		promutil.AddToCounter(c.metrics.transactionReceivedTotal, len(blk.Txs))
		c.numWaitingTxsForStatus.Add(int32(len(blk.Txs))) //nolint:gosec

		if len(blk.Txs) == 0 {
			continue
		}

		// TODO: make it configurable.
		chunkSizeForDepGraph := min(c.config.DependencyGraphConfig.WaitingTxsLimit, 500)
		for i := 0; i < len(blk.Txs); i += chunkSizeForDepGraph {
			end := min(i+chunkSizeForDepGraph, len(blk.Txs))
			txsBatchForDependencyGraph.Write(
				&dependencygraph.TransactionBatch{
					ID:          c.txBatchIDToDepGraph,
					BlockNumber: blk.Number,
					Txs:         blk.Txs[i:end],
					TxsNum:      blk.TxsNum[i:end],
				})
			c.txBatchIDToDepGraph++
		}
	}

	return nil
}

func (c *Service) sendTxStatus(
	ctx context.Context,
	stream protocoordinatorservice.Coordinator_BlockProcessingServer,
) error {
	txsStatus := channel.NewReader(ctx, c.queues.vcServiceToCoordinatorTxStatus)
	for {
		txStatus, ctxAlive := txsStatus.Read()
		if !ctxAlive {
			return nil
		}
		c.numWaitingTxsForStatus.Add(-int32(len(txStatus.Status))) //nolint:gosec

		if err := stream.Send(txStatus); err != nil {
			return errors.Wrap(err, "failed to send transaction status batch to the sidecar")
		}

		logger.Debugf("Batch with %d TX statuses forwarded to output stream.", len(txStatus.Status))

		// TODO: introduce metrics to record all sent statuses. Issue #436.
		m := c.metrics
		for _, status := range txStatus.Status {
			switch status.Code {
			case protoblocktx.Status_COMMITTED:
				promutil.AddToCounter(m.transactionCommittedStatusSentTotal, 1)
			case protoblocktx.Status_ABORTED_MVCC_CONFLICT:
				promutil.AddToCounter(m.transactionMVCCConflictStatusSentTotal, 1)
			case protoblocktx.Status_REJECTED_DUPLICATE_TX_ID:
				promutil.AddToCounter(m.transactionDuplicateTxStatusSentTotal, 1)
			case protoblocktx.Status_ABORTED_SIGNATURE_INVALID:
				promutil.AddToCounter(m.transactionInvalidSignatureStatusSentTotal, 1)
			}
		}
	}
}

func (c *Service) monitorQueues(ctx context.Context) {
	// TODO: make sampling time configurable
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		m := c.metrics
		q := c.queues
		promutil.SetGauge(m.sigverifierInputTxBatchQueueSize, len(q.depGraphToSigVerifierFreeTxs))
		promutil.SetGauge(m.sigverifierOutputValidatedTxBatchQueueSize, len(q.sigVerifierToVCServiceValidatedTxs))
		promutil.SetGauge(m.vcserviceOutputValidatedTxBatchQueueSize, len(q.vcServiceToDepGraphValidatedTxs))
		promutil.SetGauge(m.vcserviceOutputTxStatusBatchQueueSize, len(q.vcServiceToCoordinatorTxStatus))
	}
}
