package coordinatorservice

import (
	"context"
	"errors"
	"sync"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
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
		ctx                 context.Context
		cancel              context.CancelFunc
	}

	// validatorCommitter is responsible for managing the communication with a single
	// vcserver.
	validatorCommitter struct {
		stream         protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
		sendOnStreamMu sync.Mutex
		metrics        *perfMetrics

		// vc service returns only the txID and the status of the transaction. To find the
		// transaction node associated with the txID, we use txBeingValidated map.
		txBeingValidated *sync.Map

		ctx context.Context
	}

	validatorCommitterManagerConfig struct {
		serversConfig                           []*connection.ServerConfig
		incomingTxsForValidationCommit          <-chan []*dependencygraph.TransactionNode
		incomingPrelimInvalidTxsStatusForCommit <-chan []*protovcservice.Transaction
		outgoingValidatedTxsNode                chan<- []*dependencygraph.TransactionNode
		outgoingTxsStatus                       chan<- *protovcservice.TransactionStatus
		metrics                                 *perfMetrics
	}
)

func newValidatorCommitterManager(ctx context.Context, c *validatorCommitterManagerConfig) *validatorCommitterManager {
	ctx, cancel := context.WithCancel(ctx)
	return &validatorCommitterManager{
		config:              c,
		txsStatusBufferSize: cap(c.outgoingTxsStatus),
		ctx:                 ctx,
		cancel:              cancel,
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
		vc, err := newValidatorCommitter(vcm.ctx, serverConfig, c.metrics)
		if err != nil {
			return nil, err
		}
		vcm.validatorCommitter[i] = vc
		logger.Debugf("Client [%d] successfully created and connected to vc", i)

		go func() {
			errChan <- vc.sendTransactionsToVCService(channel.NewReader(vcm.ctx, c.incomingTxsForValidationCommit))
		}()

		go func() {
			errChan <- vc.sendPrelimInvalidTxsStatusToVCService(
				channel.NewReader(vcm.ctx, c.incomingPrelimInvalidTxsStatusForCommit))
		}()

		go func() {
			errChan <- vc.forwardTransactionsStatusAndTxsNode(
				channel.NewWriter(vcm.ctx, c.outgoingValidatedTxsNode),
				channel.NewWriter(vcm.ctx, c.outgoingTxsStatus),
			)
		}()
	}

	return errChan, nil
}

func (vcm *validatorCommitterManager) close() {
	vcm.cancel()
}

func newValidatorCommitter(
	ctx context.Context,
	serverConfig *connection.ServerConfig,
	metrics *perfMetrics,
) (
	*validatorCommitter, error,
) {
	conn, err := connection.Connect(connection.NewDialConfig(serverConfig.Endpoint))
	if err != nil {
		return nil, err
	}

	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	vcStream, err := client.StartValidateAndCommitStream(ctx)
	if err != nil {
		return nil, err
	}

	return &validatorCommitter{
		stream:           vcStream,
		txBeingValidated: &sync.Map{},
		metrics:          metrics,
		ctx:              ctx,
	}, nil
}

func (vc *validatorCommitter) sendTransactionsToVCService(
	inputTxsNode channel.Reader[[]*dependencygraph.TransactionNode],
) error {
	for {
		txsNode, ok := inputTxsNode.Read()
		if !ok {
			return nil
		}

		logger.Debugf("New TX node came from coordinator to vc manager")
		txBatch := make([]*protovcservice.Transaction, len(txsNode))
		for i, txNode := range txsNode {
			vc.txBeingValidated.Store(txNode.Tx.ID, txNode)
			txBatch[i] = txNode.Tx
		}

		if err := vc.sendTxs(txBatch); err != nil {
			return err
		}

		logger.Debugf("TX node contains %d TXs, and was sent to a cv", len(txBatch))
	}
}

func (vc *validatorCommitter) sendPrelimInvalidTxsStatusToVCService(
	inputPrelimInvalidTxsStatus channel.Reader[[]*protovcservice.Transaction],
) error {
	for {
		invalidTxs, ok := inputPrelimInvalidTxsStatus.Read()
		if !ok {
			return nil
		}

		if err := vc.sendTxs(invalidTxs); err != nil {
			return err
		}
	}
}

func (vc *validatorCommitter) sendTxs(txs []*protovcservice.Transaction) error {
	vc.sendOnStreamMu.Lock()
	defer vc.sendOnStreamMu.Unlock()
	return vc.stream.Send(
		&protovcservice.TransactionBatch{
			Transactions: txs,
		},
	)
}

func (vc *validatorCommitter) forwardTransactionsStatusAndTxsNode(
	outputTxsNode channel.Writer[[]*dependencygraph.TransactionNode],
	outputTxsStatus channel.Writer[*protovcservice.TransactionStatus],
) error {
	for {
		txsStatus, err := vc.stream.Recv()
		if err != nil {
			return connection.FilterStreamErrors(err)
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
		if ok := outputTxsStatus.Write(txsStatus); !ok {
			return nil
		}
		logger.Debugf("Forwarded batch with %d TX statuses back to coordinator", len(txsStatus.Status))

		vc.metrics.addToCounter(vc.metrics.vcserviceTransactionProcessedTotal, len(txsStatus.Status))

		txsNode := make([]*dependencygraph.TransactionNode, 0, len(txsStatus.Status))
		for txID := range txsStatus.Status {
			v, ok := vc.txBeingValidated.LoadAndDelete(txID)
			if !ok {
				// Transactions sent with a preliminary invalid status will not be
				// found in the txBeingValidated list. Furthermore, these transactions
				// are not part of the dependency graph and, hence, we can move to the next.
				continue
			}

			txNode, ok := v.(*dependencygraph.TransactionNode)
			if !ok {
				return errors.New("failed to cast txNode stored in the txBeingValidated map")
			}
			txsNode = append(txsNode, txNode)
		}

		if ok := outputTxsNode.Write(txsNode); !ok {
			return nil
		}
		logger.Debugf("Forwarded batch with %d TX statuses back to dep graph", len(txsStatus.Status))
	}
}
