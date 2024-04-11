package coordinatorservice

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type (
	// validatorCommitterManager is responsible for managing all communication with
	// all vcservices. It is responsible for:
	// 1. Sending transactions to be validated and committed to the vcservices.
	// 2. Receiving the status of the transactions from the vcservices.
	// 3. Forwarding the validated transactions node to the dependency graph manager.
	// 4. Forwarding the status of the transactions to the coordinator.
	validatorCommitterManager struct {
		config              *validatorCommitterManagerConfig
		validatorCommitter  []*validatorCommitter
		txsStatusBufferSize int
	}

	// validatorCommitter is responsible for managing the communication with a single
	// vcserver.
	validatorCommitter struct {
		stream           protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
		statusCollection chan *protovcservice.TransactionStatus
		metrics          *perfMetrics

		// vc service returns only the txID and the status of the transaction. To find the
		// transaction node associated with the txID, we use txBeingValidated map.
		txBeingValidated *sync.Map

		// stopGoroutines is used to stopGoroutines goroutines created by the validatorCommitter.
		stopGoroutines chan any
		// wg is used to ensure that all goroutines in the validator and committer manager
		// are terminated before closing it.
		wg sync.WaitGroup
	}

	validatorCommitterManagerConfig struct {
		serversConfig                  []*connection.ServerConfig
		incomingTxsForValidationCommit <-chan []*dependencygraph.TransactionNode
		outgoingValidatedTxsNode       chan<- []*dependencygraph.TransactionNode
		outgoingTxsStatus              chan<- *protovcservice.TransactionStatus
		metrics                        *perfMetrics
	}
)

func newValidatorCommitterManager(c *validatorCommitterManagerConfig) *validatorCommitterManager {
	return &validatorCommitterManager{
		config:              c,
		txsStatusBufferSize: cap(c.outgoingTxsStatus),
	}
}

func (vcm *validatorCommitterManager) start() (chan error, error) {
	c := vcm.config
	logger.Infof("Connections to %d vc's will be opened from vc manager", len(c.serversConfig))
	vcm.validatorCommitter = make([]*validatorCommitter, len(c.serversConfig))

	numErrorableGoroutinePerServer := 3
	errChan := make(chan error, numErrorableGoroutinePerServer*len(c.serversConfig))

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("vc manager creates client to vc [%d] listening on %s", i, serverConfig.Endpoint.String())
		vc, err := newValidatorCommitter(serverConfig, vcm.txsStatusBufferSize, c.metrics)
		if err != nil {
			return nil, err
		}
		vcm.validatorCommitter[i] = vc
		logger.Debugf("Client [%d] successfully created and connected to vc", i)

		vc.wg.Add(3)
		go func() {
			defer vc.wg.Done()
			errChan <- vc.sendTransactionsToVCService(c.incomingTxsForValidationCommit)
		}()

		go func() {
			defer vc.wg.Done()
			errChan <- vc.receiveTransactionsStatusFromVCService()
		}()

		go func() {
			defer vc.wg.Done()
			errChan <- vc.forwardTransactionsStatusAndTxsNode(
				c.outgoingValidatedTxsNode,
				c.outgoingTxsStatus,
			)
		}()
	}

	return errChan, nil
}

func (vcm *validatorCommitterManager) close() error {
	logger.Infof("Closing %d connections vc's", len(vcm.validatorCommitter))
	for _, vc := range vcm.validatorCommitter {
		if err := vc.close(); err != nil {
			return err
		}
	}

	return nil
}

func newValidatorCommitter(
	serverConfig *connection.ServerConfig,
	receivedTxsStatusBufferSize int,
	metrics *perfMetrics,
) (
	*validatorCommitter, error,
) {
	conn, err := connection.Connect(connection.NewDialConfig(serverConfig.Endpoint))
	if err != nil {
		return nil, err
	}

	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	vcStream, err := client.StartValidateAndCommitStream(context.Background())
	if err != nil {
		return nil, err
	}

	return &validatorCommitter{
		stream:           vcStream,
		statusCollection: make(chan *protovcservice.TransactionStatus, receivedTxsStatusBufferSize),
		txBeingValidated: &sync.Map{},
		metrics:          metrics,
		stopGoroutines:   make(chan any),
	}, nil
}

func (vc *validatorCommitter) sendTransactionsToVCService(
	inputTxsNode <-chan []*dependencygraph.TransactionNode,
) error {
	var txsNode []*dependencygraph.TransactionNode
	var ok bool
	for {
		select {
		case <-vc.stopGoroutines:
			return nil
		case txsNode, ok = <-inputTxsNode:
			if !ok {
				return nil
			}
		}

		logger.Debugf("New TX node came from coordinator to vc manager")
		txBatch := make([]*protovcservice.Transaction, len(txsNode))
		for i, txNode := range txsNode {
			vc.txBeingValidated.Store(txNode.Tx.ID, txNode)
			txBatch[i] = txNode.Tx
		}

		if err := vc.stream.Send(
			&protovcservice.TransactionBatch{
				Transactions: txBatch,
			},
		); err != nil {
			return err
		}
		logger.Debugf("TX node contains %d TXs, and was sent to a cv", len(txBatch))
	}
}

func (vc *validatorCommitter) receiveTransactionsStatusFromVCService() error {
	status := make(chan *protovcservice.TransactionStatus)
	streamErr := make(chan error)

	// As the stream.Recv() is blocking, we need to run it in a separate goroutine.
	// Thus, we can stop the goroutine when the stop channel is closed.
	go func() {
		for {
			txsStatus, err := vc.stream.Recv()
			if err != nil {
				streamErr <- err
				return
			}
			status <- txsStatus
		}
	}()

	for {
		select {
		case <-vc.stopGoroutines:
			return nil
		case err := <-streamErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case txsStatus := <-status:
			vc.statusCollection <- txsStatus
		}
	}
}

func (vc *validatorCommitter) forwardTransactionsStatusAndTxsNode(
	outputTxsNode chan<- []*dependencygraph.TransactionNode,
	outputTxsStatus chan<- *protovcservice.TransactionStatus,
) error {
	var txsStatus *protovcservice.TransactionStatus
	var ok bool
	for {
		select {
		case <-vc.stopGoroutines:
			return nil
		case txsStatus, ok = <-vc.statusCollection:
			if !ok {
				return nil
			}
		}

		logger.Debugf("Batch contains %d TX statuses", len(txsStatus.Status))
		// NOTE: The sidecar reads transactions from the ordering service stream and sends
		//       them to the coordinator. The coordinator then forwards the transactions to the
		//       dependency graph manager. The dependency graph manager forwards the transactions
		//       to the validator committer manager. The validator committer manager sends the
		//       transactions to the VC services. The VC services validate and commit the
		//       transactions, sending the status back to the validator committer manager.
		//       The validator committer manager then sends the status to the coordinator.
		//       The coordinator sends the status back to the sidecar. The sidecar accumulates
		//       the transaction statuses at the block level and sends them to all connected clients.
		//       There is no cycle in the producer-consumer flow. If the sidecar becomes bottlenecked
		//       and cannot receive the statuses quickly, the gRPC flow control will activate and
		//       slow down the whole system, allowing the sidecar to catch up.
		outputTxsStatus <- txsStatus
		logger.Debugf("Forwarded batch with %d TX statuses back to coordinator", len(txsStatus.Status))

		vc.metrics.addToCounter(vc.metrics.vcserviceTransactionProcessedTotal, len(txsStatus.Status))

		txsNode := make([]*dependencygraph.TransactionNode, 0, len(txsStatus.Status))
		for txID := range txsStatus.Status {
			v, ok := vc.txBeingValidated.LoadAndDelete(txID)
			if !ok {
				return errors.New("failed to load and delete txNode from the txBeingValidated map")
			}

			txNode, ok := v.(*dependencygraph.TransactionNode)
			if !ok {
				return errors.New("failed to cast txNode stored in the txBeingValidated map")
			}
			txsNode = append(txsNode, txNode)
		}

		outputTxsNode <- txsNode
		logger.Debugf("Forwarded batch with %d TX statuses back to dep graph", len(txsStatus.Status))
	}
}

func (vc *validatorCommitter) close() error {
	close(vc.stopGoroutines)
	vc.wg.Wait()
	close(vc.statusCollection)
	return vc.stream.CloseSend()
}
