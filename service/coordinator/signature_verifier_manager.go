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
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/coordinator/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
)

type (
	// signatureVerifierManager is responsible for managing all communication with
	// all signature verifier servers. It is responsible for:
	// 1. Sending transactions to be verified to the signature verifier servers.
	// 2. Receiving the status of the transactions from the signature verifier servers.
	// 3. Forwarding the status of the transactions to the coordinator.
	signatureVerifierManager struct {
		config       *signVerifierManagerConfig
		signVerifier []*signatureVerifier
		metrics      *perfMetrics
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

		policyManager *policyManager
		lifecycle     *connection.RemoteServiceLifecycle
	}

	signVerifierManagerConfig struct {
		serversConfig            []*connection.ServerConfig
		incomingTxsForValidation <-chan dependencygraph.TxNodeBatch
		outgoingValidatedTxs     chan<- dependencygraph.TxNodeBatch
		metrics                  *perfMetrics
		policyManager            *policyManager
	}
)

var sigInvalidTxStatus = &protovcservice.InvalidTxStatus{
	Code: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
}

func newSignatureVerifierManager(config *signVerifierManagerConfig) *signatureVerifierManager {
	return &signatureVerifierManager{
		config:  config,
		metrics: config.metrics,
	}
}

func (svm *signatureVerifierManager) run(ctx context.Context) error {
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
		conn, err := connection.LazyConnect(connection.NewDialConfig(&serverConfig.Endpoint))
		if err != nil {
			return errors.Wrapf(err, "failed to create connection to signature verifier [%d] at %s",
				i, &serverConfig.Endpoint)
		}
		logger.Infof("connected to signature verifier [%d] at %s", i, &serverConfig.Endpoint)
		label := conn.CanonicalTarget()
		c.metrics.verifiersConnection.Disconnected(label)

		sv := newSignatureVerifier(c, conn)
		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)

		g.Go(func() error {
			defer connection.CloseConnectionsLog(conn)
			// error should never occur unless there is a bug or malicious activity. Hence, it is fine to crash for now.
			err := sv.sendTransactionsAndForwardStatus(eCtx, txBatchQueue, channel.NewWriter(
				eCtx,
				c.outgoingValidatedTxs,
			))
			c.metrics.verifiersConnection.Disconnected(label)
			return errors.Wrap(err, "failed to send transactions and receive verification statuses from verifiers")
		})
	}
	return g.Wait()
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

func newSignatureVerifier(
	config *signVerifierManagerConfig,
	conn *grpc.ClientConn,
) *signatureVerifier {
	return &signatureVerifier{
		conn:             conn,
		client:           protosigverifierservice.NewVerifierClient(conn),
		metrics:          config.metrics,
		txBeingValidated: make(map[types.Height]*dependencygraph.TransactionNode),
		txMu:             &sync.Mutex{},
		policyManager:    config.policyManager,
		lifecycle: &connection.RemoteServiceLifecycle{
			Name:        conn.CanonicalTarget(),
			ConnMetrics: config.metrics.verifiersConnection,
			// TODO: initialize retry from config.
		},
	}
}

// sendTransactionsAndForwardStatus initiates a stream to a verifier
// and use it to send transactions, and receive the results.
// It reconnects the stream in case of failure.
// It stops when the context was cancelled, i.e., the SVM have closed, or according to the retry policy.
func (sv *signatureVerifier) sendTransactionsAndForwardStatus(
	ctx context.Context,
	inputTxBatch channel.ReaderWriter[dependencygraph.TxNodeBatch],
	outputValidatedTxs channel.Writer[dependencygraph.TxNodeBatch],
) error {
	return sv.lifecycle.RunLifecycle(ctx, func(sCtx context.Context) error {
		stream, err := sv.client.StartStream(sCtx)
		if err != nil {
			return errors.Wrap(err, "failed to start stream")
		}

		sv.lifecycle.Go(func() error { //nolint:contextcheck
			return sv.receiveStatusAndForwardToOutput(stream, outputValidatedTxs.WithContext(stream.Context()))
		})

		sv.lifecycle.Go(func() error { //nolint:contextcheck
			return sv.sendTransactionsToSVService(stream, inputTxBatch.WithContext(stream.Context()))
		})

		return nil
	}, func() error {
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
			promutil.AddToCounter(sv.metrics.verifiersRetriedTransactionTotal, len(pendingTxs))
			inputTxBatch.Write(pendingTxs)
		}
		return nil
	})
}

// NOTE: sendTransactionsToSVService filters all transient connection related errors.
func (sv *signatureVerifier) sendTransactionsToSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
	inputTxBatch channel.Reader[dependencygraph.TxNodeBatch],
) error {
	var policyVersion uint64
	for {
		txBatch, ctxAlive := inputTxBatch.Read()
		if !ctxAlive {
			return errors.Wrap(inputTxBatch.Context().Err(), "context ended")
		}

		sv.addTxsBeingValidated(txBatch)

		batchSize := len(txBatch)
		logger.Debugf("Batch containing %d TXs was stored in the being validated list", batchSize)

		request := &protosigverifierservice.RequestBatch{
			Requests: make([]*protosigverifierservice.Request, batchSize),
		}

		request.Update, policyVersion = sv.policyManager.getUpdates(policyVersion)

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

		if err := stream.Send(request); err != nil {
			return errors.Wrap(err, "send to stream ended with error")
		}
		logger.Debugf("Batch contains %d TXs, and was stored in the accumulator and sent to a sv", batchSize)
	}
}

func (sv *signatureVerifier) receiveStatusAndForwardToOutput(
	stream protosigverifierservice.Verifier_StartStreamClient,
	outputValidatedTxs channel.Writer[dependencygraph.TxNodeBatch],
) error {
	for {
		response, err := stream.Recv()
		if err != nil {
			// The stream ended or the SVM was closed.
			return errors.Wrap(err, "receive from stream ended with error")
		}

		logger.Debugf("New batch came from sv to sv manager, contains %d items", len(response.Responses))
		// We view a successful stream.Recv() as the fist indication of successful interaction.
		sv.lifecycle.ReportInteraction()

		validatedTxs := dependencygraph.TxNodeBatch{}
		// TODO: introduce metrics to measure the lock wait/holding duration.
		sv.txMu.Lock()
		for _, resp := range response.Responses {
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

		logger.Info("Waiting to add")
		if !outputValidatedTxs.Write(validatedTxs) {
			// Since transactions are loaded and deleted from txBeingValidated before their
			// validation results are queued, we must re-queue the transaction to txBeingValidated
			// if its result cannot be added to the outputValidatedTxs queue.
			sv.addTxsBeingValidated(validatedTxs)
			return errors.Wrap(outputValidatedTxs.Context().Err(), "context ended")
		}

		promutil.AddToCounter(sv.metrics.sigverifierTransactionProcessedTotal, len(response.Responses))
	}
}

func (sv *signatureVerifier) addTxsBeingValidated(txBatch dependencygraph.TxNodeBatch) {
	sv.txMu.Lock()
	for _, txNode := range txBatch {
		sv.txBeingValidated[types.Height{BlockNum: txNode.Tx.BlockNumber, TxNum: txNode.Tx.TxNum}] = txNode
	}
	sv.txMu.Unlock()
}
