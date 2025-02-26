package coordinatorservice

import (
	"context"
	"sync"

	"github.com/cockroachdb/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/grpcerror"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
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
		conn      *grpc.ClientConn
		client    protovcservice.ValidationAndCommitServiceClient
		metrics   *perfMetrics
		policyMgr *policyManager

		// vc service returns only the txID and the status of the transaction. To find the
		// transaction node associated with the txID, we use txBeingValidated map.
		txBeingValidated *sync.Map
	}

	validatorCommitterManagerConfig struct {
		serversConfig                  []*connection.ServerConfig
		incomingTxsForValidationCommit <-chan dependencygraph.TxNodeBatch
		outgoingValidatedTxsNode       chan<- dependencygraph.TxNodeBatch
		outgoingTxsStatus              chan<- *protoblocktx.TransactionsStatus
		metrics                        *perfMetrics
		policyMgr                      *policyManager
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

	g, eCtx := errgroup.WithContext(ctx)

	txBatchQueue := channel.NewReaderWriter(eCtx,
		make(chan dependencygraph.TxNodeBatch, cap(c.incomingTxsForValidationCommit)))
	g.Go(func() error {
		ingestIncomingTxsToInternalQueue(
			channel.NewReader(eCtx, c.incomingTxsForValidationCommit),
			txBatchQueue,
		)
		return nil
	})

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("vc manager creates client to vc [%d] listening on %s", i, &serverConfig.Endpoint)
		vc, err := newValidatorCommitter(serverConfig, c.metrics, c.policyMgr) //nolint:contextcheck // issue #693
		if err != nil {
			return errors.Wrapf(err, "failed to create validator client with %s", serverConfig.Endpoint.Address())
		}
		logger.Debugf("Client [%d] successfully created and connected to vc", i)
		vcm.validatorCommitter[i] = vc

		g.Go(func() error {
			err := vc.sendTransactionsAndForwardStatus(
				eCtx,
				txBatchQueue,
				channel.NewWriter(eCtx, c.outgoingValidatedTxsNode),
				channel.NewWriter(eCtx, c.outgoingTxsStatus),
			)
			return errors.Wrap(err, "failed to send transactions and receive commit status from validator-committers")
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

	return errors.Wrapf(err, "failed to set the last committed block number [%d]", lastBlock.Number)
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

	return nil, errors.Wrap(err, "failed to get the last committed block number")
}

func (vcm *validatorCommitterManager) getTransactionsStatus(
	ctx context.Context,
	query *protoblocktx.QueryStatus,
) (*protoblocktx.TransactionsStatus, error) {
	var err error
	var status *protoblocktx.TransactionsStatus
	for _, vc := range vcm.validatorCommitter {
		status, err = vc.client.GetTransactionsStatus(ctx, query)
		if err == nil {
			return status, nil
		}
	}

	return nil, errors.Wrap(err, "failed to get transactions status")
}

func (vcm *validatorCommitterManager) getPolicies(
	ctx context.Context,
) error {
	var errs []error
	for _, vc := range vcm.validatorCommitter {
		policyMsg, err := vc.client.GetPolicies(ctx, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return vcm.config.policyMgr.updatePolicies(ctx, policyMsg)
	}
	return errors.Wrap(errors.Join(errs...), "failed loading policy: %w")
}

func newValidatorCommitter(serverConfig *connection.ServerConfig, metrics *perfMetrics, policyMgr *policyManager) (
	*validatorCommitter, error,
) {
	conn, err := connection.Connect(connection.NewDialConfig(&serverConfig.Endpoint))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create connection to validator persister running at %s",
			&serverConfig.Endpoint)
	}
	logger.Infof("validator persister manager connected to validator persister at %s", &serverConfig.Endpoint)

	client := protovcservice.NewValidationAndCommitServiceClient(conn)

	return &validatorCommitter{
		conn:             conn,
		client:           client,
		metrics:          metrics,
		policyMgr:        policyMgr,
		txBeingValidated: &sync.Map{},
	}, nil
}

func (vc *validatorCommitter) sendTransactionsAndForwardStatus(
	ctx context.Context,
	inputTxBatch channel.ReaderWriter[dependencygraph.TxNodeBatch],
	outputValidatedTxsNode channel.Writer[dependencygraph.TxNodeBatch],
	outputTxsStatus channel.Writer[*protoblocktx.TransactionsStatus],
) error {
	for ctx.Err() == nil {
		// Re-enter pending transactions to the queue so other workers can fetch them.
		pendingTxs := dependencygraph.TxNodeBatch{}
		var err error
		vc.txBeingValidated.Range(func(_, v any) bool {
			txNode, ok := v.(*dependencygraph.TransactionNode)
			if !ok {
				err = errors.New("failed to cast txNode stored in the txBeingValidated map")
				return false
			}
			pendingTxs = append(pendingTxs, txNode)
			return true
		})
		if err != nil {
			return errors.Wrap(err, "failed to start sender and receiver of validator-committer")
		}
		vc.txBeingValidated = &sync.Map{}

		if len(pendingTxs) > 0 {
			inputTxBatch.Write(pendingTxs)
		}

		waitForConnection(ctx, vc.conn)

		sCtx, sCancel := context.WithCancel(ctx)
		stream, err := vc.client.StartValidateAndCommitStream(sCtx)
		if err != nil {
			logger.Warnf("Failed starting stream with error: %s", err)
			sCancel()
			continue
		}
		logger.Debug("VC stream is connected")

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() { //nolint:contextcheck
			defer wg.Done()
			vc.sendTransactionsToVCService(stream, inputTxBatch.WithContext(stream.Context()))
		}()

		// NOTE: The channels outputValidatedTxsNode and outputTxsStatus should not depend on the stream context.
		//       Doing so can result in permanently lost validation results. Specifically, after reading a
		//       transaction from the stream and removing it from txBeingValidated, if the stream context is
		//       canceled before we can write to these two channels, the validation results are lost forever.
		//       Similarly, the first argument, i.e., context should not be stream context.
		err = vc.receiveStatusAndForwardToOutput(ctx, stream, outputValidatedTxsNode, outputTxsStatus)
		sCancel() // if receiveStatusAndForwardToOutput fails due to an internal error in
		// the verifier, the sendTransactionsAndForwardStatus wouldn't return unless we cancel the stream context.

		wg.Wait()
		if err != nil {
			logger.ErrorStackTrace(err)
			return errors.Wrap(err, "failed to receive status and forward to output")
		}
		logger.Debug("Stream ended: %s", err)
	}

	return nil
}

func (vc *validatorCommitter) sendTransactionsToVCService(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
	inputTxsNode channel.Reader[dependencygraph.TxNodeBatch],
) {
	for {
		txsNode, ok := inputTxsNode.Read()
		if !ok {
			logger.Warnf("context has ended: %s", inputTxsNode.Context().Err())
			return
		}

		logger.Debugf("New TX node came from dependency graph manager to vc manager")
		txBatch := make([]*protovcservice.Transaction, len(txsNode))
		for i, txNode := range txsNode {
			vc.txBeingValidated.Store(txNode.Tx.ID, txNode)
			txBatch[i] = txNode.Tx
		}

		err := stream.Send(&protovcservice.TransactionBatch{
			Transactions: txBatch,
		})
		if err != nil {
			// The stream ended or the VCM was closed.
			logger.Warnf("Receive from stream ended with error: %s. Reconnecting.", err)
			return
		}
		logger.Debugf("TX node contains %d TXs, and was sent to a vcservice", len(txBatch))
	}
}

// NOTE: receiveStatusAndForwardToOutput filters all transient connection related errors.
func (vc *validatorCommitter) receiveStatusAndForwardToOutput( //nolint:gocognit
	ctx context.Context,
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
	outputTxsNode channel.Writer[dependencygraph.TxNodeBatch],
	outputTxsStatus channel.Writer[*protoblocktx.TransactionsStatus],
) error {
	for {
		txsStatus, err := stream.Recv()
		if err != nil {
			// The stream ended or the SVM was closed.
			logger.Warnf("Receive from stream ended with error: %s. Reconnecting.", err)
			return nil
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
		for txID, status := range txsStatus.Status {
			v, ok := vc.txBeingValidated.LoadAndDelete(txID)
			if !ok {
				// Because the VC manager might submit the same transaction multiple times (for example,
				// if a VC service fails or the coordinator reconnects to a failed VC service), it could
				// receive duplicate responses.  However, the txBeingValidated lookup will succeed only once.
				// Therefore, if the transaction ID is not found in txBeingValidated, we must proceed to
				// the next status.
				continue
			}

			txNode, ok := v.(*dependencygraph.TransactionNode)
			if !ok {
				// NOTE: This error should never occur.
				return errors.New("failed to cast txNode stored in the txBeingValidated map")
			}
			txsNode = append(txsNode, txNode)

			if status.Code != protoblocktx.Status_COMMITTED {
				continue
			}

			// NOTE: Updating policy before sending transaction nodes to the dependency
			//       graph manager to free dependent transactions. Otherwise, dependent transactions
			//       might be validated against a stale policy.
			if err := vc.policyMgr.updatePoliciesFromTx(ctx, txNode.Tx.Namespaces); err != nil {
				logger.ErrorStackTrace(err)
				return errors.Wrap(grpcerror.FilterUnavailableErrorCode(err),
					"failed to update policy after processing a transaction")
			}
		}

		if len(txsNode) > 0 && !outputTxsNode.Write(txsNode) {
			return nil
		}
		logger.Debugf("Forwarded batch with %d TX statuses back to dep graph", len(txsStatus.Status))
	}
}
