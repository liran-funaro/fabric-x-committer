package coordinator

import (
	"context"
	"sync"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/coordinator/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
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
		// ready indicates that the validatorCommitter array is initialized.
		ready *channel.Ready
	}

	// validatorCommitter is responsible for managing the communication with a single
	// vcserver.
	validatorCommitter struct {
		conn      *grpc.ClientConn
		client    protovcservice.ValidationAndCommitServiceClient
		metrics   *perfMetrics
		policyMgr *policyManager
		lifecycle *RemoteServiceLifecycle

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
		ready:               channel.NewReady(),
	}
}

func (vcm *validatorCommitterManager) run(ctx context.Context) error {
	defer vcm.ready.Reset()
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
		label := []string{vc.conn.CanonicalTarget()}
		promutil.SetGaugeVec(vc.metrics.vcservicesConnectionStatus, label, connection.Connected)

		logger.Debugf("Client [%d] successfully created and connected to vc", i)
		vcm.validatorCommitter[i] = vc

		g.Go(func() error {
			err := vc.sendTransactionsAndForwardStatus(
				eCtx,
				txBatchQueue,
				channel.NewWriter(eCtx, c.outgoingValidatedTxsNode),
				channel.NewWriter(eCtx, c.outgoingTxsStatus),
			)
			_ = vc.conn.Close() // it does not matter whether it returns an error
			promutil.SetGaugeVec(vc.metrics.vcservicesConnectionStatus, []string{vc.conn.CanonicalTarget()},
				connection.Disconnected)
			return errors.Wrap(err, "failed to send transactions and receive commit status from validator-committers")
		})
	}

	vcm.ready.SignalReady()
	return g.Wait()
}

func (vcm *validatorCommitterManager) setLastCommittedBlockNumber(
	ctx context.Context,
	lastBlock *protoblocktx.BlockInfo,
) error {
	_, err := utils.FirstSuccessful(
		vcm.validatorCommitter,
		func(vc *validatorCommitter) (*protovcservice.Empty, error) {
			return vc.client.SetLastCommittedBlockNumber(ctx, lastBlock)
		},
	)
	return errors.Wrap(err, "failed setting the last committed block number")
}

func (vcm *validatorCommitterManager) getLastCommittedBlockNumber(
	ctx context.Context,
) (*protoblocktx.BlockInfo, error) {
	ret, err := utils.FirstSuccessful(
		vcm.validatorCommitter,
		func(vc *validatorCommitter) (*protoblocktx.BlockInfo, error) {
			return vc.client.GetLastCommittedBlockNumber(ctx, nil)
		},
	)
	return ret, errors.Wrap(err, "failed getting the last committed block number")
}

func (vcm *validatorCommitterManager) getTransactionsStatus(
	ctx context.Context,
	query *protoblocktx.QueryStatus,
) (*protoblocktx.TransactionsStatus, error) {
	ret, err := utils.FirstSuccessful(
		vcm.validatorCommitter,
		func(vc *validatorCommitter) (*protoblocktx.TransactionsStatus, error) {
			return vc.client.GetTransactionsStatus(ctx, query)
		},
	)
	return ret, errors.Wrap(err, "failed getting transactions status")
}

func (vcm *validatorCommitterManager) getNamespacePolicies(
	ctx context.Context,
) (*protoblocktx.NamespacePolicies, error) {
	ret, err := utils.FirstSuccessful(
		vcm.validatorCommitter,
		func(vc *validatorCommitter) (*protoblocktx.NamespacePolicies, error) {
			return vc.client.GetNamespacePolicies(ctx, nil)
		},
	)
	return ret, errors.Wrap(err, "failed loading policies")
}

func (vcm *validatorCommitterManager) getConfigTransaction(
	ctx context.Context,
) (*protoblocktx.ConfigTransaction, error) {
	ret, err := utils.FirstSuccessful(
		vcm.validatorCommitter,
		func(vc *validatorCommitter) (*protoblocktx.ConfigTransaction, error) {
			return vc.client.GetConfigTransaction(ctx, nil)
		},
	)
	return ret, errors.Wrap(err, "failed loading config transaction")
}

func (vcm *validatorCommitterManager) recoverPolicyManagerFromStateDB(ctx context.Context) error {
	policyMsg, err := vcm.getNamespacePolicies(ctx)
	if err != nil {
		return err
	}
	configMsg, err := vcm.getConfigTransaction(ctx)
	if err != nil {
		return err
	}
	if len(policyMsg.Policies) == 0 && configMsg.Envelope == nil {
		return nil
	}
	vcm.config.policyMgr.update(&protosigverifierservice.Update{
		NamespacePolicies: policyMsg,
		Config:            configMsg,
	})
	return nil
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
		lifecycle: &RemoteServiceLifecycle{
			Name:         conn.CanonicalTarget(),
			ConnStatus:   metrics.vcservicesConnectionStatus,
			FailureTotal: metrics.vcservicesConnectionFailureTotal,
			// TODO: initialize retry from config.
		},
	}, nil
}

func (vc *validatorCommitter) sendTransactionsAndForwardStatus(
	ctx context.Context,
	inputTxBatch channel.ReaderWriter[dependencygraph.TxNodeBatch],
	outputValidatedTxsNode channel.Writer[dependencygraph.TxNodeBatch],
	outputTxsStatus channel.Writer[*protoblocktx.TransactionsStatus],
) error {
	return vc.lifecycle.RunLifecycle(ctx, func(sCtx context.Context) error {
		stream, err := vc.client.StartValidateAndCommitStream(sCtx)
		if err != nil {
			return errors.Wrap(err, "failed to start stream")
		}
		//nolint:contextcheck
		vc.lifecycle.Go(func() error {
			return vc.sendTransactionsToVCService(stream, inputTxBatch.WithContext(stream.Context()))
		})
		vc.lifecycle.Go(func() error {
			// NOTE: The channels outputValidatedTxsNode and outputTxsStatus should not depend on the stream context.
			//       Doing so can result in permanently lost validation results. Specifically, after reading a
			//       transaction from the stream and removing it from txBeingValidated, if the stream context is
			//       canceled before we can write to these two channels, the validation results are lost forever.
			//       Similarly, the first argument, i.e., context should not be stream context.
			return vc.receiveStatusAndForwardToOutput(stream, outputValidatedTxsNode, outputTxsStatus)
		})
		return nil
	}, func() error {
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
			promutil.AddToCounter(vc.metrics.vcservicesRetriedTransactionTotal, len(pendingTxs))
			inputTxBatch.Write(pendingTxs)
		}
		return nil
	})
}

func (vc *validatorCommitter) sendTransactionsToVCService(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
	inputTxsNode channel.Reader[dependencygraph.TxNodeBatch],
) error {
	for {
		txsNode, ok := inputTxsNode.Read()
		if !ok {
			return errors.Wrap(inputTxsNode.Context().Err(), "context ended")
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
			return errors.Wrap(err, "receive from stream ended with error")
		}
		logger.Debugf("TX node contains %d TXs, and was sent to a vcservice", len(txBatch))
	}
}

// NOTE: receiveStatusAndForwardToOutput filters all transient connection related errors.
func (vc *validatorCommitter) receiveStatusAndForwardToOutput( //nolint:gocognit
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
	outputTxsNode channel.Writer[dependencygraph.TxNodeBatch],
	outputTxsStatus channel.Writer[*protoblocktx.TransactionsStatus],
) error {
	for {
		txsStatus, err := stream.Recv()
		if err != nil {
			// The stream ended or the SVM was closed.
			return errors.Wrap(err, "receive from stream ended with error")
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
			return errors.Wrap(outputTxsStatus.Context().Err(), "context ended")
		}
		logger.Debugf("Forwarded batch with %d TX statuses back to coordinator", len(txsStatus.Status))

		promutil.AddToCounter(vc.metrics.vcserviceTransactionProcessedTotal, len(txsStatus.Status))

		txsNode := make([]*dependencygraph.TransactionNode, 0, len(txsStatus.Status))
		for txID, txStatus := range txsStatus.Status {
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
				return errors.Wrap(ErrLifecycleCritical, "failed to cast txNode from the txBeingValidated map")
			}
			txsNode = append(txsNode, txNode)

			if txStatus.Code != protoblocktx.Status_COMMITTED {
				continue
			}

			// Updating policy before sending transaction nodes to the dependency
			// graph manager to free dependent transactions. Otherwise, dependent transactions
			// might be validated against a stale policy.
			vc.policyMgr.updateFromTx(txNode.Tx.Namespaces)
		}

		if len(txsNode) > 0 && !outputTxsNode.Write(txsNode) {
			return errors.Wrap(outputTxsNode.Context().Err(), "context ended")
		}
		logger.Debugf("Forwarded batch with %d TX statuses back to dep graph", len(txsStatus.Status))
	}
}
