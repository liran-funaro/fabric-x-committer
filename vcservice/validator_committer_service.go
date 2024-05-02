package vcservice

import (
	"context"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/prometheusmetrics"
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
	txBatchChan              chan *protovcservice.TransactionBatch
	preparedTxsChan          chan *preparedTransactions
	validatedTxsChan         chan *validatedTransactions
	txsStatusChan            chan *protovcservice.TransactionStatus
	db                       *database
	metrics                  *perfMetrics
	promErrChan              <-chan error
	minTxBatchSize           int
	timeoutForMinTxBatchSize time.Duration
	ctx                      context.Context
	ctxCancel                context.CancelFunc
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
	txBatch := make(chan *protovcservice.TransactionBatch, l.MaxWorkersForPreparer*queueMultiplier)
	preparedTxs := make(chan *preparedTransactions, l.MaxWorkersForValidator*queueMultiplier)
	validatedTxs := make(chan *validatedTransactions, queueMultiplier)
	txsStatus := make(chan *protovcservice.TransactionStatus, l.MaxWorkersForCommitter*queueMultiplier)

	metrics := newVCServiceMetrics()
	db, err := newDatabase(config.Database, metrics)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	vc := &ValidatorCommitterService{
		ctx:                      ctx,
		ctxCancel:                cancel,
		preparer:                 newPreparer(ctx, txBatch, preparedTxs, metrics),
		validator:                newValidator(ctx, db, preparedTxs, validatedTxs, metrics),
		committer:                newCommitter(ctx, db, validatedTxs, txsStatus, metrics),
		txBatchChan:              txBatch,
		preparedTxsChan:          preparedTxs,
		validatedTxsChan:         validatedTxs,
		txsStatusChan:            txsStatus,
		db:                       db,
		metrics:                  metrics,
		minTxBatchSize:           config.ResourceLimits.MinTransactionBatchSize,
		timeoutForMinTxBatchSize: 5 * time.Second,
	}

	vc.promErrChan = metrics.provider.StartPrometheusServer(config.Monitoring.Metrics.Endpoint)
	go vc.monitorQueues()

	logger.Infof("Starting %d workers for the transaction preparer", l.MaxWorkersForPreparer)
	vc.preparer.start(l.MaxWorkersForPreparer)

	logger.Infof("Starting %d workers for the transaction validator", l.MaxWorkersForValidator)
	vc.validator.start(l.MaxWorkersForValidator)

	logger.Infof("Starting %d workers for the transaction committer", l.MaxWorkersForCommitter)
	vc.committer.start(l.MaxWorkersForCommitter)

	return vc, nil
}

func (vc *ValidatorCommitterService) monitorQueues() {
	// TODO: make sampling time configurable
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-vc.ctx.Done():
			return
		case <-ticker.C:
		case err := <-vc.promErrChan:
			// Once the prometheus server is stopped, we no longer need to monitor the queues.
			logger.Errorf("Prometheus ended with error: %s", err)
			return
		}
		prometheusmetrics.SetQueueSize(vc.metrics.preparerInputQueueSize, len(vc.txBatchChan))
		prometheusmetrics.SetQueueSize(vc.metrics.validatorInputQueueSize, len(vc.preparedTxsChan))
		prometheusmetrics.SetQueueSize(vc.metrics.committerInputQueueSize, len(vc.validatedTxsChan))
		prometheusmetrics.SetQueueSize(vc.metrics.txStatusOutputQueueSize, len(vc.txsStatusChan))
	}
}

// StartValidateAndCommitStream is the function that starts the stream between the client and the service.
// It receives transactions from the client, prepares them, validates them and commits them to the database.
// It also sends the status of the transactions to the client.
func (vc *ValidatorCommitterService) StartValidateAndCommitStream(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	logger.Info("Start validate and commit stream")

	errorChannel := make(chan error)

	go func() {
		logger.Info("Started a goroutine to receive and process transactions")
		errorChannel <- vc.receiveAndProcessTransactions(stream)
	}()

	go func() {
		logger.Info("Started a goroutine to send transaction status to the submitter")
		errorChannel <- vc.sendTransactionStatus(stream)
	}()

	err := <-errorChannel
	if err != nil {
		logger.Error(err)
	}

	return nil
}

// receiveAndProcessTransactions receives transactions from the client, prepares them,
// validates them and commits them to the database.
func (vc *ValidatorCommitterService) receiveAndProcessTransactions(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	txBatchChan := make(chan *protovcservice.TransactionBatch)
	streamErr := make(chan error)
	go func() {
		for {
			b, err := stream.Recv()
			if err != nil {
				streamErr <- err
				return
			}
			txBatchChan <- b
		}
	}()

	largerBatch := &protovcservice.TransactionBatch{}
	sendLargeBatch := func() {
		defer func() {
			// If the context ended, then we don't care about the panic.
			// Specifically, panic that resulted from a closed channel.
			if vc.ctx.Err() != nil {
				_ = recover()
			}
		}()
		vc.txBatchChan <- largerBatch
		prometheusmetrics.AddToCounter(vc.metrics.transactionReceivedTotal, len(largerBatch.Transactions))
		largerBatch = &protovcservice.TransactionBatch{}
	}

	timer := time.NewTimer(vc.timeoutForMinTxBatchSize)
	defer timer.Stop()
	for {
		select {
		case <-vc.ctx.Done():
			return nil
		case err := <-streamErr:
			return connection.WrapStreamRpcError(err)
		case <-timer.C:
			if len(largerBatch.Transactions) > 0 {
				sendLargeBatch()
				timer.Reset(vc.timeoutForMinTxBatchSize)
			}
		case txBatch, ok := <-txBatchChan:
			if !ok {
				return nil
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
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	logger.Info("Send transaction status")

	ctx := stream.Context()
	var txStatus *protovcservice.TransactionStatus

	for {
		select {
		case <-ctx.Done():
			return nil
		case txStatus = <-vc.txsStatusChan:
		}

		if txStatus == nil {
			// txsStatusChan is closed
			return nil
		}

		err := stream.Send(txStatus)
		if err != nil {
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

// Close stops the VC service and blocks until it is done.
func (vc *ValidatorCommitterService) Close() {
	vc.ctxCancel()

	err := vc.metrics.provider.StopServer()
	if err != nil {
		logger.Errorf("Failed stopping prometheus server: %s", err)
	}

	logger.Info("Closing VC service output channel")
	close(vc.txBatchChan)

	logger.Info("Waiting for preparer to finish")
	vc.preparer.wg.Wait()

	logger.Info("Closing preparer output channel")
	close(vc.preparedTxsChan)

	logger.Info("Waiting for validator to finish")
	vc.validator.wg.Wait()

	logger.Info("Closing validator output channel")
	close(vc.validatedTxsChan)

	logger.Info("Waiting for committer to finish")
	vc.committer.wg.Wait()

	logger.Info("Closing committer output channel")
	close(vc.txsStatusChan)

	logger.Info("Closing the database connection")
	vc.db.close()
}
