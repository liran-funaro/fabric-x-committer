package coordinatorservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
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
		connectionReady chan any
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
	}

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
		connectionReady: make(chan any),
	}
}

func (svm *signatureVerifierManager) run(ctx context.Context) error {
	defer func() { svm.connectionReady = make(chan any) }()
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
		sv, err := newSignatureVerifier(serverConfig, svm.metrics)
		if err != nil {
			return err
		}

		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)
		g.Go(func() error {
			sv.sendTransactionsAndForwardStatus(eCtx, txBatchQueue, channel.NewWriter(eCtx, c.outgoingValidatedTxs))
			return nil
		})
	}

	close(svm.connectionReady)
	return g.Wait()
}

func (svm *signatureVerifierManager) updatePolicies(
	ctx context.Context,
	policies *protosigverifierservice.Policies,
) error {
	for i, sv := range svm.signVerifier {
		logger.Infof("Updating policy for sv [%d]", i)
		_, err := sv.client.UpdatePolicies(ctx, policies)
		if err != nil {
			return err
		}
		logger.Infof("Policies update successfully for sv [%d]", i)
	}
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
		return nil, fmt.Errorf(
			"failed to create connection to signature verifier running at %s: %w",
			&serverConfig.Endpoint,
			err,
		)
	}
	logger.Infof("signature verifier manager connected to signature verifier at %s", &serverConfig.Endpoint)

	return &signatureVerifier{
		conn:             conn,
		client:           protosigverifierservice.NewVerifierClient(conn),
		metrics:          metrics,
		txBeingValidated: make(map[types.Height]*dependencygraph.TransactionNode),
		txMu:             &sync.Mutex{},
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
) {
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
		stream, err := sv.client.StartStream(ctx)
		if err != nil {
			logger.Warnf("Failed starting stream with error: %s", err)
			continue
		}
		logger.Debugf("SV stream is connected")

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			// NOTE: outputValidatedTxs should not use the stream context.
			// Otherwise, there is a possibility of losing validation results forever.
			sv.receiveStatusAndForwardToOutput(stream, outputValidatedTxs)
		}()
		sv.sendTransactionsToSVService(stream, inputTxBatch.WithContext(stream.Context()))
		wg.Wait()
		logger.Debug("Stream ended")
	}
}

// waitForConnection returns when SV is connected or the SVM have closed.
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
			logger.Debug("Reconnecting SV.")
			conn.Connect()
			if conn.GetState() == connectivity.Ready {
				return
			}
			logger.Debug("SV connection is not ready.")
		}
	}
}

func (sv *signatureVerifier) sendTransactionsToSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
	inputTxBatch channel.Reader[dependencygraph.TxNodeBatch],
) {
	for {
		txBatch, ctxAlive := inputTxBatch.Read()
		if !ctxAlive {
			return
		}

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

		if err := stream.Send(request); err != nil {
			logger.Warnf("Send to stream ended with error: %s. Reconnecting.", err)
			return
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
			return
		}

		sv.metrics.addToCounter(sv.metrics.sigverifierTransactionProcessedTotal, len(response.Responses))
	}
}
