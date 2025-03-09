package coordinatorservice

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/grpcerror"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type (
	// signatureVerifierManager is responsible for managing all communication with
	// all signature verifier servers. It is responsible for:
	// 1. Sending transactions to be verified to the signature verifier servers.
	// 2. Receiving the status of the transactions from the signature verifier servers.
	// 3. Forwarding the status of the transactions to the coordinator.
	signatureVerifierManager struct {
		config          *signVerifierManagerConfig
		signVerifier    []*signatureVerifier
		metrics         *perfMetrics
		connectionReady *channel.Ready
	}

	// signatureVerifier is responsible for managing the communication with a single
	// signature verifier server.
	signatureVerifier struct {
		conn    *grpc.ClientConn
		client  protosigverifierservice.VerifierClient
		metrics *perfMetrics

		// txBeingValidated stores transactions currently being validated by the signature verifier.
		// The key is the Height (block number, transaction index), and the value is the
		// dependencygraph.TransactionNode. If signature verifier service fails, these transactions are
		// requeued to the input queue for processing by other signature verifiers.
		txBeingValidated map[types.Height]*dependencygraph.TransactionNode
		txMu             *sync.Mutex

		// pendingNsPolicies indicates whether a policy update is pending. We do not maintain a detailed
		// list of pending policies; instead, we send all policies because verifier restarts and transient
		// connection issues occur very infrequently. This infrequency allows us to simplify the logic.
		pendingNsPolicies bool
		allNsPolicies     nsToPolicy
		nsPolicyMu        *sync.Mutex
	}

	nsToPolicy map[string]*protoblocktx.PolicyItem

	signVerifierManagerConfig struct {
		serversConfig            []*connection.ServerConfig
		incomingTxsForValidation <-chan dependencygraph.TxNodeBatch
		outgoingValidatedTxs     chan<- dependencygraph.TxNodeBatch
		metrics                  *perfMetrics
	}
)

var sigInvalidTxStatus = &protovcservice.InvalidTxStatus{
	Code: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
}

func newSignatureVerifierManager(config *signVerifierManagerConfig) *signatureVerifierManager {
	return &signatureVerifierManager{
		config:          config,
		metrics:         config.metrics,
		connectionReady: channel.NewReady(),
	}
}

func (svm *signatureVerifierManager) run(ctx context.Context) error {
	defer svm.connectionReady.Reset()
	c := svm.config
	logger.Infof("Connections to %d sv's will be opened from sv manager", len(c.serversConfig))
	svm.signVerifier = make([]*signatureVerifier, len(c.serversConfig))

	derivedCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, eCtx := errgroup.WithContext(derivedCtx)

	txBatchQueue := channel.NewReaderWriter(eCtx, make(chan dependencygraph.TxNodeBatch, cap(c.outgoingValidatedTxs)))
	g.Go(func() error {
		ingestIncomingTxsToInternalQueue(
			channel.NewReader(eCtx, c.incomingTxsForValidation),
			txBatchQueue,
		)
		return nil
	})

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("sv manager creates client to sv [%d] listening on %s", i, &serverConfig.Endpoint)
		sv, err := newSignatureVerifier(serverConfig, svm.metrics) //nolint:contextcheck // issue #693
		if err != nil {
			return errors.Wrapf(err, "failed to create verifier client with %s", serverConfig.Endpoint.Address())
		}

		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)
		g.Go(func() error {
			// error should never occur unless there is a bug or malicious activity. Hence, it is fine to crash for now.
			err := sv.sendTransactionsAndForwardStatus(eCtx, txBatchQueue, channel.NewWriter(
				eCtx,
				c.outgoingValidatedTxs,
			))
			return errors.Wrap(err, "failed to send transactions and receive verification statuses from verifiers")
		})
	}

	svm.connectionReady.SignalReady()
	return g.Wait()
}

func (svm *signatureVerifierManager) updatePolicies(
	ctx context.Context,
	policies *protoblocktx.Policies,
) error {
	for i, sv := range svm.signVerifier {
		logger.Infof("Updating policy for sv [%d]", i)

		err := grpcerror.FilterUnavailableErrorCode(sv.updatePolicies(ctx, false, policies.Policies...))
		if err != nil {
			logger.ErrorStackTrace(err)
			// we reach here only if we receive an internal error but it is quite rare.
			return errors.Wrap(err, "failed to update verifiers with new policies")
		}
	}

	// NOTE: Returning immediately is safe, even if some or all verifiers have not yet been updated
	//       with the latest policies. Once the connection is re-established, sendTransactionsToSVService
	//       will update the verifier; without this update, the requests would not be sent. Returning now
	//       prompts the vcservice manager to forward the transaction node to the dependency graph,
	//       thereby releasing any transactions that depend on it. These transactions will not be validated
	//       against outdated policies because sendTransactionsToSVService updates the verifier with the pending
	//       policies before sending the verification requests. If the update fails, the requests will eventually
	//       be requeued. However, if the connection is not re-established promptly, the transaction queue
	//       may eventually fill up, potentially throttling the flow from the sidecar to the coordinator.
	//       Even so, re-establishing a single verifier connection will allow transaction commits to progress.

	return nil
}

func ingestIncomingTxsToInternalQueue(
	incomingTxBatch channel.Reader[dependencygraph.TxNodeBatch],
	txsQueue channel.Writer[dependencygraph.TxNodeBatch],
) {
	for {
		txs, ctxAlive := incomingTxBatch.Read()
		if !ctxAlive {
			return
		}

		batchSize := len(txs)
		logger.Debugf("New transaction batch (size: %d) received", batchSize)

		txsQueue.Write(txs)
	}
}

func newSignatureVerifier(serverConfig *connection.ServerConfig, metrics *perfMetrics) (*signatureVerifier, error) {
	conn, err := connection.Connect(connection.NewDialConfig(&serverConfig.Endpoint))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create connection to signature verifier running at %s: %w",
			&serverConfig.Endpoint)
	}
	logger.Infof("signature verifier manager connected to signature verifier at %s", &serverConfig.Endpoint)

	return &signatureVerifier{
		conn:             conn,
		client:           protosigverifierservice.NewVerifierClient(conn),
		metrics:          metrics,
		txBeingValidated: make(map[types.Height]*dependencygraph.TransactionNode),
		txMu:             &sync.Mutex{},
		allNsPolicies:    make(nsToPolicy),
		nsPolicyMu:       &sync.Mutex{},
	}, nil
}

// sendTransactionsAndReceiveStatus initiates a stream to a verifier
// and use it to send transactions, and receive the results.
// It reconnects the stream in case of failure.
// It stops only when the context was cancelled, i.e., the SVM have closed.
func (sv *signatureVerifier) sendTransactionsAndForwardStatus(
	ctx context.Context,
	inputTxBatch channel.ReaderWriter[dependencygraph.TxNodeBatch],
	outputValidatedTxs channel.Writer[dependencygraph.TxNodeBatch],
) error {
	for ctx.Err() == nil {
		// Re-enter pending transactions to the queue so other workers can fetch them.
		pendingTxs := dependencygraph.TxNodeBatch{}
		sv.txMu.Lock()
		for txHeight, txNode := range sv.txBeingValidated {
			logger.Debugf("Recovering tx: %v", txHeight)
			pendingTxs = append(pendingTxs, txNode)
		}
		sv.txBeingValidated = make(map[types.Height]*dependencygraph.TransactionNode)
		sv.txMu.Unlock()

		if len(pendingTxs) > 0 {
			inputTxBatch.Write(pendingTxs)
		}

		waitForConnection(ctx, sv.conn)

		// NOTE: To simplify the implementation, we do not distinguish between transient connection failures
		//       and verifier process failures/restarts. Consequently, every time a connection is established
		//       with a signature verifier, we send all namespace policies. This may cause the verifier to
		//       unnecessarily create new namespace verifiers, but the tradeoff is acceptable as it is an
		//       infrequent operation.
		if err := grpcerror.FilterUnavailableErrorCode(sv.updatePolicies(ctx, true)); err != nil {
			logger.ErrorStackTrace(err)
			return errors.Wrap(err, "failed to update verifiers with new policies")
		}
		// If the failure is due to a transient connection issue, pendingNsPolicies will update the verifier.
		// Otherwise, if it's not a transient issue, the subsequent start stream execution will fail, and the
		// connection will be retried.

		sCtx, sCancel := context.WithCancel(ctx)
		stream, err := sv.client.StartStream(sCtx)
		if err != nil {
			logger.Warnf("Failed starting stream with error: %s", err)
			sCancel()
			continue
		}
		logger.Debugf("SV stream is connected")

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// NOTE: outputValidatedTxs should not use the stream context.
			//       Otherwise, there is a possibility of losing validation results forever.
			sv.receiveStatusAndForwardToOutput(stream, outputValidatedTxs)
			logger.Warnf("receiveStatusAndForwardToOutput returned")
		}()

		//nolint:contextcheck // we are passing a non-inherited context. If we need to send an
		// inherited context such as sCtx, we should make the above goroutine to explicitly
		// cancel the context so that the sendTransactionsAndForwardStatus can terminate. We defer
		// this to issue #704 as we might use errgroup and error categorization which might
		// make this linter happy.
		err = sv.sendTransactionsToSVService(stream, inputTxBatch.WithContext(stream.Context()))
		sCancel() // if sendTransactionsAndForwardStatus fails due to an internal error in
		// the verifier, the receiveStatusAndForwardToOutput wouldn't return unless we cancel the stream context.

		wg.Wait()
		if err != nil {
			logger.ErrorStackTrace(err)
			return errors.Wrap(err, "failed sending transactions to verifier") // reaching here is rare
		}
		logger.Debug("Stream ended")
	}

	return nil
}

func waitForConnection(ctx context.Context, conn *grpc.ClientConn) {
	if conn.GetState() == connectivity.Ready {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Debug("Reconnecting to service.")
			conn.Connect()
			if conn.GetState() == connectivity.Ready {
				return
			}
			logger.Debug("service connection is not ready.")
		}
	}
}

// NOTE: sendTransactionsToSVService filters all transient connection related errors.
func (sv *signatureVerifier) sendTransactionsToSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
	inputTxBatch channel.Reader[dependencygraph.TxNodeBatch],
) error {
	for {
		logger.Debug("waiting to read from inputTxBatch")
		txBatch, ctxAlive := inputTxBatch.Read()
		if !ctxAlive {
			logger.Warnf("context has ended: %s", inputTxBatch.Context().Err())
			return nil
		}
		logger.Debug("done reading")

		// TODO: introduce metrics to measure the lock wait/holding duration.
		sv.txMu.Lock()
		for _, txNode := range txBatch {
			sv.txBeingValidated[types.Height{BlockNum: txNode.Tx.BlockNumber, TxNum: txNode.Tx.TxNum}] = txNode
		}
		sv.txMu.Unlock()

		batchSize := len(txBatch)
		logger.Debugf("Batch containing %d TXs was stored in the being validated list", batchSize)

		request := &protosigverifierservice.RequestBatch{
			Requests: make([]*protosigverifierservice.Request, batchSize),
		}
		for idx, txNode := range txBatch {
			request.Requests[idx] = &protosigverifierservice.Request{
				BlockNum: txNode.Tx.BlockNumber,
				TxNum:    uint64(txNode.Tx.TxNum),
				Tx: &protoblocktx.Tx{
					Id:         txNode.Tx.ID,
					Namespaces: txNode.Tx.Namespaces,
					Signatures: txNode.Signatures,
				},
			}
		}

		// NOTE: Dependent transactions are released only after the policy has been updated.
		//       In rare cases, the updatePolicy method on the SVM might enqueue policy updates due
		//       to connection issues with this verifier, eventually releasing the dependent transactions.
		//       As a result, these transactions could be provided as input to the same verifier client.
		//       To avoid using an outdated policy, any pending policy updates must be cleared before
		//       sending requests to the verifier.
		//       Pending policies, unsent due to a transient connection failure, are released here.
		//       This failure may have been isolated from the stream.
		if err := sv.updatePolicies(stream.Context(), false); err != nil {
			logger.ErrorStackTrace(err)
			return errors.Wrap(grpcerror.FilterUnavailableErrorCode(err), "failed to update policies in verifier")
		}

		if err := stream.Send(request); err != nil {
			logger.Warnf("Send to stream ended with error: %s. Reconnecting.", err)
			return nil
		}
		logger.Debugf("Batch contains %d TXs, and was stored in the accumulator and sent to a sv", batchSize)
	}
}

func (sv *signatureVerifier) receiveStatusAndForwardToOutput(
	stream protosigverifierservice.Verifier_StartStreamClient,
	outputValidatedTxs channel.Writer[dependencygraph.TxNodeBatch],
) {
	for {
		response, err := stream.Recv()
		if err != nil {
			// The stream ended or the SVM was closed.
			logger.Warnf("Receive from stream ended with error: %s. Reconnecting.", err)
			return
		}

		logger.Debugf("New batch came from sv to sv manager, contains %d items", len(response.Responses))
		validatedTxs := dependencygraph.TxNodeBatch{}
		// TODO: introduce metrics to measure the lock wait/holding duration.
		sv.txMu.Lock()
		for _, resp := range response.Responses {
			// Since transactions are loaded and deleted before validation results are added to the output queues,
			// a signature verifier failure after processing a response should not cause this function to return.
			// As a result, the caller must ensure that outputValidatedTxs does not use the
			// signatureVerifier's stream context.
			k := types.Height{BlockNum: resp.BlockNum, TxNum: uint32(resp.TxNum)} //nolint:gosec
			txNode, ok := sv.txBeingValidated[k]
			if !ok {
				continue
			}
			delete(sv.txBeingValidated, k)
			if resp.Status != protoblocktx.Status_COMMITTED {
				txNode.Tx.PrelimInvalidTxStatus = &protovcservice.InvalidTxStatus{Code: resp.Status}
			}
			validatedTxs = append(validatedTxs, txNode)
		}
		sv.txMu.Unlock()

		if !outputValidatedTxs.Write(validatedTxs) {
			logger.Warnf("context has ended: %s", outputValidatedTxs.Context().Err())
			return
		}

		monitoring.AddToCounter(sv.metrics.sigverifierTransactionProcessedTotal, len(response.Responses))
	}
}

//nolint:revive // control flag is used to force update all policies.
func (sv *signatureVerifier) updatePolicies(
	ctx context.Context,
	forceUpdateAllPolicies bool,
	newPolicyItems ...*protoblocktx.PolicyItem,
) error {
	sv.nsPolicyMu.Lock()
	defer sv.nsPolicyMu.Unlock()
	for _, p := range newPolicyItems {
		sv.allNsPolicies[p.Namespace] = p
	}

	if sv.pendingNsPolicies || forceUpdateAllPolicies {
		newPolicyItems = []*protoblocktx.PolicyItem{}
		for ns, pItem := range sv.allNsPolicies { // allNsPolicies holds both the pending and new policies
			newPolicyItems = append(newPolicyItems, &protoblocktx.PolicyItem{
				Namespace: ns,
				Policy:    pItem.Policy,
			})
		}
	}

	sv.pendingNsPolicies = false
	if len(newPolicyItems) == 0 {
		return nil
	}

	_, err := sv.client.UpdatePolicies(ctx, &protoblocktx.Policies{Policies: newPolicyItems})
	if err == nil {
		return nil
	}

	sv.pendingNsPolicies = true
	return errors.Wrap(err, "failed to update a verifier")
}
