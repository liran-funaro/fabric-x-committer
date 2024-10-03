package coordinatorservice

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
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
		connectionReady     chan any
	}

	// validatorCommitter is responsible for managing the communication with a single
	// vcserver.
	validatorCommitter struct {
		client         protovcservice.ValidationAndCommitServiceClient
		stream         protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient
		sendOnStreamMu sync.Mutex
		metrics        *perfMetrics

		// vc service returns only the txID and the status of the transaction. To find the
		// transaction node associated with the txID, we use txBeingValidated map.
		txBeingValidated *sync.Map
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

func newValidatorCommitterManager(c *validatorCommitterManagerConfig) *validatorCommitterManager {
	return &validatorCommitterManager{
		config:              c,
		txsStatusBufferSize: cap(c.outgoingTxsStatus),
		connectionReady:     make(chan any),
	}
}

func (vcm *validatorCommitterManager) run(ctx context.Context) error {
	defer func() { vcm.connectionReady = make(chan any) }()
	c := vcm.config
	logger.Infof("Connections to %d vc's will be opened from vc manager", len(c.serversConfig))
	vcm.validatorCommitter = make([]*validatorCommitter, len(c.serversConfig))

	derivedCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, eCtx := errgroup.WithContext(derivedCtx)

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("vc manager creates client to vc [%d] listening on %s", i, serverConfig.Endpoint.String())
		vc, err := newValidatorCommitter(derivedCtx, serverConfig, c.metrics)
		if err != nil {
			return err
		}
		vcm.validatorCommitter[i] = vc
		logger.Debugf("Client [%d] successfully created and connected to vc", i)

		g.Go(func() error {
			return vc.sendTransactionsToVCService(channel.NewReader(eCtx, c.incomingTxsForValidationCommit))
		})

		g.Go(func() error {
			return vc.sendPrelimInvalidTxsStatusToVCService(
				channel.NewReader(eCtx, c.incomingPrelimInvalidTxsStatusForCommit))
		})

		g.Go(func() error {
			return vc.forwardTransactionsStatusAndTxsNode(
				channel.NewWriter(eCtx, c.outgoingValidatedTxsNode),
				channel.NewWriter(eCtx, c.outgoingTxsStatus),
			)
		})
	}

	close(vcm.connectionReady)
	return g.Wait()
}

func (vcm *validatorCommitterManager) setLastCommittedBlockNumber(
	ctx context.Context,
	lastBlock *protoblocktx.BlockInfo,
) error {
	var err error
	for _, vc := range vcm.validatorCommitter {
		if _, err = vc.client.SetLastCommittedBlockNumber(ctx, lastBlock); err == nil {
			return nil
		}
	}

	return fmt.Errorf("failed to set the last committed block number [%d]: %w", lastBlock.Number, err)
}

func (vcm *validatorCommitterManager) getLastCommittedBlockNumber(
	ctx context.Context,
) (*protoblocktx.BlockInfo, error) {
	var err error
	var lastBlock *protoblocktx.BlockInfo
	for _, vc := range vcm.validatorCommitter {
		lastBlock, err = vc.client.GetLastCommittedBlockNumber(ctx, nil)
		if err == nil {
			return lastBlock, nil
		}
	}

	return nil, fmt.Errorf("failed to get the last committed block number: %w", err)
}

func (vcm *validatorCommitterManager) getMaxSeenBlockNumber(
	ctx context.Context,
) (*protoblocktx.BlockInfo, error) {
	var err error
	var lastBlock *protoblocktx.BlockInfo
	for _, vc := range vcm.validatorCommitter {
		lastBlock, err = vc.client.GetMaxSeenBlockNumber(ctx, nil)
		if err == nil {
			return lastBlock, nil
		}
	}

	return nil, fmt.Errorf("failed to get the max seen block number: %w", err)
}

func (vcm *validatorCommitterManager) getTransactionsStatus(
	ctx context.Context,
	txIDs []string,
) (*protovcservice.TransactionStatus, error) {
	var err error
	var status *protovcservice.TransactionStatus
	for _, vc := range vcm.validatorCommitter {
		status, err = vc.client.GetTransactionsStatus(ctx, &protoblocktx.QueryStatus{TxIDs: txIDs})
		if err == nil {
			return status, nil
		}
	}

	return nil, fmt.Errorf("failed to get transactions status: %w", err)
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
	logger.Infof("validator persister manager connected to validator persister at %s", serverConfig.Endpoint.String())

	client := protovcservice.NewValidationAndCommitServiceClient(conn)

	for {
		waitingTxs, statusErr := client.NumberOfWaitingTransactionsForStatus(ctx, nil)
		if statusErr != nil {
			return nil, err
		}
		if waitingTxs.Count == 0 {
			break
		}
		logger.Infof(
			"Waiting for vcservice [%s] to complete processing [%d] pending transactions",
			&serverConfig.Endpoint,
			waitingTxs.Count,
		)
		time.Sleep(100 * time.Millisecond)
	}

	vcStream, err := client.StartValidateAndCommitStream(ctx)
	if err != nil {
		return nil, err
	}

	return &validatorCommitter{
		client:           client,
		stream:           vcStream,
		txBeingValidated: &sync.Map{},
		metrics:          metrics,
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

		logger.Debugf("New TX node came from dependency graph manager to vc manager")
		txBatch := make([]*protovcservice.Transaction, len(txsNode))
		for i, txNode := range txsNode {
			vc.txBeingValidated.Store(txNode.Tx.ID, txNode)
			txBatch[i] = txNode.Tx
		}

		if err := vc.sendTxs(txBatch); err != nil {
			return err
		}

		logger.Debugf("TX node contains %d TXs, and was sent to a vcservice", len(txBatch))
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
			return err
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
