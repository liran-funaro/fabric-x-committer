package vcservice

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
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

	vc := &ValidatorCommitterService{
		preparer:                 newPreparer(txBatch, preparedTxs, metrics),
		validator:                newValidator(db, preparedTxs, validatedTxs, metrics),
		committer:                newCommitter(db, validatedTxs, txsStatus, metrics),
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
	for {
		select {
		case <-ticker.C:
		case err := <-vc.promErrChan:
			// Once the prometheus server is stopped, we no longer need to monitor the queues.
			ticker.Stop()
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

// isStreamEndError detects error that are caused due a closed stream.
func isStreamEndError(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// receiveAndProcessTransactions receives transactions from the client, prepares them,
// validates them and commits them to the database.
func (vc *ValidatorCommitterService) receiveAndProcessTransactions(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	largerBatch := &protovcservice.TransactionBatch{}
	var mu sync.Mutex

	timer := time.NewTimer(vc.timeoutForMinTxBatchSize)

	sendLargeBatch := func() {
		prometheusmetrics.AddToCounter(vc.metrics.transactionReceivedTotal, len(largerBatch.Transactions))
		txs := largerBatch
		vc.txBatchChan <- txs
		largerBatch = &protovcservice.TransactionBatch{}
		timer.Reset(vc.timeoutForMinTxBatchSize)
	}

	go func() {
		for {
			<-timer.C
			mu.Lock()
			if len(largerBatch.GetTransactions()) > 0 {
				sendLargeBatch()
			}
			mu.Unlock()
		}
	}()

	for {
		txBatch, err := stream.Recv()
		if err != nil {
			logger.Error(err)
			if isStreamEndError(err) {
				return nil
			}
			return err
		}

		mu.Lock()
		largerBatch.Transactions = append(largerBatch.Transactions, txBatch.Transactions...)
		if len(largerBatch.GetTransactions()) < vc.minTxBatchSize {
			mu.Unlock()
			continue
		}

		sendLargeBatch()
		mu.Unlock()
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
			logger.Error(err)
			if isStreamEndError(err) {
				return nil
			}
			return err
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

func (vc *ValidatorCommitterService) close() {
	err := vc.metrics.provider.StopServer()
	if err != nil {
		logger.Errorf("Failed stopping prometheus server: %s", err)
	}

	logger.Info("Stopping the transaction preparer workers")
	close(vc.txBatchChan)

	logger.Info("Stopping the transaction validator workers")
	close(vc.preparedTxsChan)

	logger.Info("Stopping the transaction committer workers")
	close(vc.validatedTxsChan)

	logger.Info("Stopping the transaction status sender")
	close(vc.txsStatusChan)

	logger.Info("Closing the database connection")
	vc.db.close()
}
