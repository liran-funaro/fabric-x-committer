package vcservice

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
	"golang.org/x/sync/errgroup"
)

var logger = logging.New("validator and committer service")

// ValidatorCommitterService is the service that receives transactions from the client, prepares them,
// validates them and commits them to the database.
// It is composed of a preparer, a validator and a committer.
// The preparer receives transactions from the client and prepares them for validation.
// The validator receives prepared transactions from the preparer and validates them.
// The committer receives validated transactions from the validator and commits them to the database.
// The service also sends the status of the transactions to the client.
// ValidatorCommitterService is a gRPC service that implements the ValidationAndCommitService interface.
type ValidatorCommitterService struct {
	protovcservice.UnimplementedValidationAndCommitServiceServer
	preparer                 *transactionPreparer
	validator                *transactionValidator
	committer                *transactionCommitter
	receivedTxBatch          chan *protovcservice.TransactionBatch
	toPrepareTxs             chan *protovcservice.TransactionBatch
	preparedTxs              chan *preparedTransactions
	validatedTxs             chan *validatedTransactions
	txsStatus                chan *protovcservice.TransactionStatus
	db                       *database
	metrics                  *perfMetrics
	promErrChan              <-chan error
	minTxBatchSize           int
	timeoutForMinTxBatchSize time.Duration
	numWaitingTxsForStatus   atomic.Int32
	config                   *ValidatorCommitterServiceConfig

	// isStreamActive indicates whether a stream from the client (i.e., coordinator) to the vcservice
	// is currently active. We permit a maximum of one active stream at a time. If multiple
	// streams were allowed concurrently, transaction status might not reach the client
	// reliably, as we are not currently associating requests and their corresponding responses
	// (i.e., status updates) with specific streams.
	// Further, when isStreamActive is active, NumberOfWaitingTransactionsForStatus would return an
	// error as this gRPC api can be called only when the stream is inactive.
	isStreamActive atomic.Bool
}

// Limits is the struct that contains the limits of the service.
type Limits struct {
	MaxWorkersForPreparer  int
	MaxWorkersForValidator int
	MaxWorkersForCommitter int
}

// NewValidatorCommitterService creates a new ValidatorCommitterService.
// It creates the preparer, the validator and the committer.
// It also creates the channels that are used to communicate between the preparer, the validator and the committer.
// It also creates the database connection.
func NewValidatorCommitterService(config *ValidatorCommitterServiceConfig) (*ValidatorCommitterService, error) {
	l := config.ResourceLimits

	// TODO: make queueMultiplier configurable
	queueMultiplier := 1
	receivedTxBatch := make(chan *protovcservice.TransactionBatch, l.MaxWorkersForPreparer*queueMultiplier)
	toPrepareTxs := make(chan *protovcservice.TransactionBatch, l.MaxWorkersForPreparer*queueMultiplier)
	preparedTxs := make(chan *preparedTransactions, l.MaxWorkersForValidator*queueMultiplier)
	validatedTxs := make(chan *validatedTransactions, queueMultiplier)
	txsStatus := make(chan *protovcservice.TransactionStatus, l.MaxWorkersForCommitter*queueMultiplier)

	metrics := newVCServiceMetrics()
	db, err := newDatabase(config.Database, metrics)
	if err != nil {
		return nil, err
	}

	vc := &ValidatorCommitterService{
		preparer:                 newPreparer(toPrepareTxs, preparedTxs, metrics),
		validator:                newValidator(db, preparedTxs, validatedTxs, metrics),
		committer:                newCommitter(db, validatedTxs, txsStatus, metrics),
		receivedTxBatch:          receivedTxBatch,
		toPrepareTxs:             toPrepareTxs,
		preparedTxs:              preparedTxs,
		validatedTxs:             validatedTxs,
		txsStatus:                txsStatus,
		db:                       db,
		metrics:                  metrics,
		minTxBatchSize:           config.ResourceLimits.MinTransactionBatchSize,
		timeoutForMinTxBatchSize: config.ResourceLimits.TimeoutForMinTransactionBatchSize,
		config:                   config,
	}

	return vc, nil
}

// Run starts the validator and committer service.
func (vc *ValidatorCommitterService) Run(ctx context.Context) error {
	g, eCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		_ = vc.metrics.provider.StartPrometheusServer(
			eCtx, vc.config.Monitoring.Metrics.Endpoint, vc.monitorQueues,
		)
		// We don't return error here to avoid stopping the service due to monitoring error.
		// But we use the errgroup to ensure the method returns only when the server exits.
		return nil
	})

	g.Go(func() error {
		vc.batchReceivedTransactionsAndForwardForProcessing(eCtx)
		return nil
	})

	l := vc.config.ResourceLimits
	logger.Infof("Starting %d workers for the transaction preparer", l.MaxWorkersForPreparer)
	g.Go(func() error {
		return vc.preparer.run(eCtx, l.MaxWorkersForPreparer)
	})

	logger.Infof("Starting %d workers for the transaction validator", l.MaxWorkersForValidator)
	g.Go(func() error {
		return vc.validator.run(eCtx, l.MaxWorkersForValidator)
	})

	logger.Infof("Starting %d workers for the transaction committer", l.MaxWorkersForCommitter)
	g.Go(func() error {
		return vc.committer.run(eCtx, l.MaxWorkersForCommitter)
	})

	if err := g.Wait(); err != nil {
		logger.Errorf("vcservice processing has been stopped due to err [%v]", err)
		return err
	}
	return nil
}

func (vc *ValidatorCommitterService) monitorQueues(ctx context.Context) {
	// TODO: make sampling time configurable
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case err := <-vc.promErrChan:
			// Once the prometheus server is stopped, we no longer need to monitor the queues.
			logger.Errorf("Prometheus ended with error: %s", err)
			return
		}
		prometheusmetrics.SetQueueSize(vc.metrics.preparerInputQueueSize, len(vc.toPrepareTxs))
		prometheusmetrics.SetQueueSize(vc.metrics.validatorInputQueueSize, len(vc.preparedTxs))
		prometheusmetrics.SetQueueSize(vc.metrics.committerInputQueueSize, len(vc.validatedTxs))
		prometheusmetrics.SetQueueSize(vc.metrics.txStatusOutputQueueSize, len(vc.txsStatus))
	}
}

// SetLastCommittedBlockNumber set the last committed block number in the database/ledger.
func (vc *ValidatorCommitterService) SetLastCommittedBlockNumber(
	ctx context.Context,
	lastCommittedBlock *protoblocktx.BlockInfo,
) (*protovcservice.Empty, error) {
	if lastCommittedBlock == nil {
		return nil, fmt.Errorf("the last committed block number is not set")
	}
	return nil, vc.db.setLastCommittedBlockNumber(ctx, lastCommittedBlock)
}

// GetLastCommittedBlockNumber get the last committed block number in the database/ledger.
func (vc *ValidatorCommitterService) GetLastCommittedBlockNumber(
	ctx context.Context,
	_ *protovcservice.Empty,
) (*protoblocktx.BlockInfo, error) {
	return vc.db.getLastCommittedBlockNumber(ctx)
}

// GetTransactionsStatus gets the status of a given set of transaction IDs.
func (vc *ValidatorCommitterService) GetTransactionsStatus(
	ctx context.Context,
	query *protoblocktx.QueryStatus,
) (*protovcservice.TransactionStatus, error) {
	txIDs := make([][]byte, len(query.GetTxIDs()))
	for i, txID := range query.GetTxIDs() {
		txIDs[i] = []byte(txID)
	}
	return vc.db.queryTransactionsStatus(ctx, txIDs)
}

// StartValidateAndCommitStream is the function that starts the stream between the client and the service.
// It receives transactions from the client, prepares them, validates them and commits them to the database.
// It also sends the status of the transactions to the client.
func (vc *ValidatorCommitterService) StartValidateAndCommitStream(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	if !vc.isStreamActive.CompareAndSwap(false, true) {
		return utils.ErrActiveStream
	}
	defer vc.isStreamActive.Store(false)

	g, ctx := errgroup.WithContext(stream.Context())

	g.Go(func() error {
		logger.Info("Started a goroutine to receive and process transactions")
		return vc.receiveTransactions(ctx, stream)
	})

	g.Go(func() error {
		logger.Info("Started a goroutine to send transaction status to the submitter")
		return vc.sendTransactionStatus(ctx, stream)
	})

	return g.Wait()
}

// NumberOfWaitingTransactionsForStatus returns the number of transactions waiting to get the final status.
func (vc *ValidatorCommitterService) NumberOfWaitingTransactionsForStatus(
	_ context.Context,
	_ *protovcservice.Empty,
) (*protovcservice.WaitingTransactions, error) {
	if vc.isStreamActive.Load() {
		return nil, fmt.Errorf("stream is still active." +
			"NumberOfWaitingTransactionsForStatus should be called only when the stream is inactive")
	}

	return &protovcservice.WaitingTransactions{
		Count: vc.numWaitingTxsForStatus.Load() - int32(len(vc.txsStatus)),
	}, nil
}

func (vc *ValidatorCommitterService) receiveTransactions(
	ctx context.Context,
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	for ctx.Err() == nil {
		b, err := stream.Recv()
		if err != nil {
			return connection.WrapStreamRpcError(err)
		}
		txCount := len(b.Transactions)
		vc.numWaitingTxsForStatus.Add(int32(txCount))
		prometheusmetrics.AddToCounter(vc.metrics.transactionReceivedTotal, txCount)
		vc.receivedTxBatch <- b
	}

	return nil
}

func (vc *ValidatorCommitterService) batchReceivedTransactionsAndForwardForProcessing(ctx context.Context) {
	largerBatch := &protovcservice.TransactionBatch{}
	timer := time.NewTimer(vc.timeoutForMinTxBatchSize)
	defer timer.Stop()
	toPrepareTxs := channel.NewWriter(ctx, vc.toPrepareTxs)

	sendLargeBatch := func() {
		if len(largerBatch.Transactions) == 0 {
			return
		}
		if ok := toPrepareTxs.Write(largerBatch); !ok {
			return
		}
		largerBatch = &protovcservice.TransactionBatch{}
		timer.Reset(vc.timeoutForMinTxBatchSize)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			sendLargeBatch()
		case txBatch, ok := <-vc.receivedTxBatch:
			if !ok {
				return
			}
			largerBatch.Transactions = append(largerBatch.Transactions, txBatch.Transactions...)
			logger.Debugf("New batch with %d TXs received in vc."+
				" Large batch contains %d TXs and the minimum batch size is %d",
				len(txBatch.Transactions), len(txBatch.Transactions)+len(largerBatch.Transactions),
				vc.minTxBatchSize)
			if len(largerBatch.Transactions) >= vc.minTxBatchSize {
				sendLargeBatch()
			}
		}
	}
}

// sendTransactionStatus sends the status of the transactions to the client.
func (vc *ValidatorCommitterService) sendTransactionStatus(
	ctx context.Context,
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	logger.Info("Send transaction status")

	txsStatus := channel.NewReader(ctx, vc.txsStatus)
	for {
		txStatus, ok := txsStatus.Read()
		if !ok {
			return nil
		}
		vc.numWaitingTxsForStatus.Add(-int32(len(txStatus.Status)))

		if err := stream.Send(txStatus); err != nil {
			return connection.WrapStreamRpcError(err)
		}

		committed := 0
		mvcc := 0
		dup := 0
		for _, status := range txStatus.Status {
			switch status {
			case protoblocktx.Status_COMMITTED:
				committed++
			case protoblocktx.Status_ABORTED_MVCC_CONFLICT:
				mvcc++
			case protoblocktx.Status_ABORTED_DUPLICATE_TXID:
				dup++
			}
		}

		prometheusmetrics.AddToCounter(vc.metrics.transactionCommittedTotal, committed)
		prometheusmetrics.AddToCounter(vc.metrics.transactionMVCCConflictTotal, mvcc)
		prometheusmetrics.AddToCounter(vc.metrics.transactionDuplicateTxTotal, dup)
		prometheusmetrics.AddToCounter(vc.metrics.transactionProcessedTotal, len(txStatus.Status))
	}
}

// Close stops the VC service.
func (vc *ValidatorCommitterService) Close() {
	logger.Info("Closing the database connection")
	vc.db.close()
}
