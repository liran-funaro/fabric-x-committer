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
		serversConfig                        []*connection.ServerConfig
		validatorCommitter                   []*validatorCommitter
		incomingTxsNodeForValidationCommit   <-chan []*dependencygraph.TransactionNode
		outgoingTxsNodeAfterValidationCommit chan<- []*dependencygraph.TransactionNode
		outgoingTxsStatus                    chan<- *protovcservice.TransactionStatus
		txsStatusBufferSize                  int
	}

	// validatorCommitter is responsible for managing the communication with a single
	// vcserver.
	validatorCommitter struct {
		stream           protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
		statusCollection chan *protovcservice.TransactionStatus

		// vc service returns only the txID and the status of the transaction. To find the
		// transaction node associated with the txID, we use txBeingValidated map.
		txBeingValidated *sync.Map
	}

	validatorCommitterManagerConfig struct {
		serversConfig                  []*connection.ServerConfig
		incomingTxsForValidationCommit <-chan []*dependencygraph.TransactionNode
		outgoingValidatedTxsNode       chan<- []*dependencygraph.TransactionNode
		outgoingTxsStatus              chan<- *protovcservice.TransactionStatus
		internalTxsStatusBufferSize    int
	}
)

func newValidatorCommitterManager(c *validatorCommitterManagerConfig) *validatorCommitterManager {
	return &validatorCommitterManager{
		serversConfig:                        c.serversConfig,
		incomingTxsNodeForValidationCommit:   c.incomingTxsForValidationCommit,
		outgoingTxsNodeAfterValidationCommit: c.outgoingValidatedTxsNode,
		outgoingTxsStatus:                    c.outgoingTxsStatus,
		txsStatusBufferSize:                  c.internalTxsStatusBufferSize,
	}
}

func (vcm *validatorCommitterManager) start() (chan error, error) {
	vcm.validatorCommitter = make([]*validatorCommitter, len(vcm.serversConfig))

	numErrorableGoroutinePerServer := 3
	errChan := make(chan error, numErrorableGoroutinePerServer*len(vcm.serversConfig))

	for i, serverConfig := range vcm.serversConfig {
		vc, err := newValidatorCommitter(serverConfig, vcm.txsStatusBufferSize)
		if err != nil {
			return nil, err
		}
		vcm.validatorCommitter[i] = vc

		go func() {
			errChan <- vc.sendTransactionsToVCService(vcm.incomingTxsNodeForValidationCommit)
		}()

		go func() {
			errChan <- vc.receiveTransactionsStatusFromVCService()
		}()

		go func() {
			errChan <- vc.forwardTransactionsStatusAndTxsNode(
				vcm.outgoingTxsNodeAfterValidationCommit,
				vcm.outgoingTxsStatus,
			)
		}()
	}

	return errChan, nil
}

func (vcm *validatorCommitterManager) close() error {
	for _, vc := range vcm.validatorCommitter {
		if err := vc.close(); err != nil {
			return err
		}
	}

	return nil
}

func newValidatorCommitter(serverConfig *connection.ServerConfig, receivedTxsStatusBufferSize int) (
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
	}, nil
}

func (vc *validatorCommitter) sendTransactionsToVCService(
	inputTxsNode <-chan []*dependencygraph.TransactionNode,
) error {
	for txsNode := range inputTxsNode {
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
	}

	return nil
}

func (vc *validatorCommitter) receiveTransactionsStatusFromVCService() error {
	for {
		txsStatus, err := vc.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		vc.statusCollection <- txsStatus
	}
}

func (vc *validatorCommitter) forwardTransactionsStatusAndTxsNode(
	outputTxsNode chan<- []*dependencygraph.TransactionNode,
	outputTxsStatus chan<- *protovcservice.TransactionStatus,
) error {
	for txsStatus := range vc.statusCollection {
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
	}

	return nil
}

func (vc *validatorCommitter) close() error {
	close(vc.statusCollection)
	return vc.stream.CloseSend()
}
