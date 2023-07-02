package vcservice

import (
	"context"
	"fmt"
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

// DBConfig is the struct that contains the configuration of the database.
type DBConfig struct {
	Host     string
	Port     int
	User     string
	Password string
}

// DataSourceName returns the data source name of the database.
func (d *DBConfig) DataSourceName() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s sslmode=disable", d.Host, d.Port, d.User, d.Password)
}

// NewValidatorCommitterService creates a new ValidatorCommitterService.
// It creates the preparer, the validator and the committer.
// It also creates the channels that are used to communicate between the preparer, the validator and the committer.
// It also creates the database connection.
func NewValidatorCommitterService(limits *Limits, dbConfig *DBConfig) *ValidatorCommitterService {
	txBatch := make(chan *protovcservice.TransactionBatch, limits.MaxWorkersForPreparer)
	preparedTxs := make(chan *preparedTransactions, limits.MaxWorkersForValidator)
	validatedTxs := make(chan *validatedTransactions, limits.MaxWorkersForCommitter)
	txsStatus := make(chan *protovcservice.TransactionStatus, limits.MaxWorkersForCommitter)

	logger.Info("Connecting to the database")
	conn, err := pgx.Connect(context.Background(), dbConfig.DataSourceName())
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

	logger.Infof("Starting %d workers for the transaction preparer", limits.MaxWorkersForPreparer)
	vc.preparer.start(limits.MaxWorkersForPreparer)

	logger.Infof("Starting %d workers for the transaction validator", limits.MaxWorkersForValidator)
	vc.validator.start(limits.MaxWorkersForValidator)

	logger.Infof("Starting %d workers for the transaction committer", limits.MaxWorkersForCommitter)
	vc.committer.start(limits.MaxWorkersForCommitter)

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
}
