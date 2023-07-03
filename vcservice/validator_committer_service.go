package vcservice

import (
	"context"
	"io"
	"log"

	"github.com/yugabyte/pgx/v4"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
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
	preparer           *transactionPreparer
	validator          *transactionValidator
	committer          *transactionCommitter
	txBatchChan        chan *protovcservice.TransactionBatch
	preparedTxsChan    chan *preparedTransactions
	validatedTxsChan   chan *validatedTransactions
	txsStatusChan      chan *protovcservice.TransactionStatus
	databaseConnection *pgx.Conn
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
func NewValidatorCommitterService(config *ValidatorCommitterServiceConfig) *ValidatorCommitterService {
	l := config.ResourceLimits
	txBatch := make(chan *protovcservice.TransactionBatch, l.MaxWorkersForPreparer)
	preparedTxs := make(chan *preparedTransactions, l.MaxWorkersForValidator)
	validatedTxs := make(chan *validatedTransactions, l.MaxWorkersForCommitter)
	txsStatus := make(chan *protovcservice.TransactionStatus, l.MaxWorkersForCommitter)

	logger.Info("Connecting to the database")
	conn, err := pgx.Connect(context.Background(), config.Database.DataSourceName())
	if err != nil {
		log.Fatal(err)
	}

	vc := &ValidatorCommitterService{
		preparer:           newPreparer(txBatch, preparedTxs),
		validator:          newValidator(conn, preparedTxs, validatedTxs),
		committer:          newCommitter(conn, validatedTxs, txsStatus),
		txBatchChan:        txBatch,
		preparedTxsChan:    preparedTxs,
		validatedTxsChan:   validatedTxs,
		txsStatusChan:      txsStatus,
		databaseConnection: conn,
	}

	logger.Infof("Starting %d workers for the transaction preparer", l.MaxWorkersForPreparer)
	vc.preparer.start(l.MaxWorkersForPreparer)

	logger.Infof("Starting %d workers for the transaction validator", l.MaxWorkersForValidator)
	vc.validator.start(l.MaxWorkersForValidator)

	logger.Infof("Starting %d workers for the transaction committer", l.MaxWorkersForCommitter)
	vc.committer.start(l.MaxWorkersForCommitter)

	return vc
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
		return err
	}

	return nil
}

// receiveAndProcessTransactions receives transactions from the client, prepares them,
// validates them and commits them to the database.
func (vc *ValidatorCommitterService) receiveAndProcessTransactions(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	for {
		txBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			logger.Error(err)
			return err
		}

		vc.txBatchChan <- txBatch
	}
}

// sendTransactionStatus sends the status of the transactions to the client.
func (vc *ValidatorCommitterService) sendTransactionStatus(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamServer,
) error {
	logger.Info("Send transaction status")

	for {
		txStatus := <-vc.txsStatusChan

		if txStatus == nil {
			// txsStatusChan is closed
			return nil
		}

		err := stream.Send(txStatus)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			logger.Error(err)
			return err
		}
	}
}

func (vc *ValidatorCommitterService) close() {
	logger.Info("Stopping the transaction preparer workers")
	close(vc.txBatchChan)

	logger.Info("Stopping the transaction validator workers")
	close(vc.preparedTxsChan)

	logger.Info("Stopping the transaction committer workers")
	close(vc.validatedTxsChan)

	logger.Info("Stopping the transaction status sender")
	close(vc.txsStatusChan)

	logger.Info("Closing the database connection")
	if err := vc.databaseConnection.Close(context.Background()); err != nil {
		logger.Errorf("Failed to close the connection to database: %v", err)
	}
}
