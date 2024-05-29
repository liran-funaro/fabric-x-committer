package coordinatorservice

import (
	"context"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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
		config       *signVerifierManagerConfig
		signVerifier []*signatureVerifier

		// ctx and cancelFunc is responsible to stop all SV streams once the SVM is closed.
		ctx        context.Context
		cancelFunc context.CancelFunc
	}

	// signatureVerifier is responsible for managing the communication with a single
	// signature verifier server.
	signatureVerifier struct {
		ctx                                   context.Context
		conn                                  *grpc.ClientConn
		client                                protosigverifierservice.VerifierClient
		incomingBlockForSignatureVerification <-chan *protoblocktx.Block

		// resultAccumulator is used to accumulate the result of the transactions
		// at block level from the signature verifier server.
		// The key is the block number and the value is the blockWithResult.
		// This accumulator is needed because the signature verifier server
		// does not guarantee the order of the responses.
		resultAccumulator *sync.Map

		// validatedBlock holds the block that has been validated by the signatureVerifier.
		// Once all transactions in the block have been validated, the block along with the
		// result will be placed on this channel.
		validatedBlock chan *blockWithResult
		metrics        *perfMetrics
	}

	txValidityStatus int
	blockWithResult  struct {
		block *protoblocktx.Block
		// txStatus maps tx-index to txValidityStatus.
		txStatus           []txValidityStatus
		pendingResultCount int
		validTxCount       int
		invalidTxCount     int
	}

	signVerifierManagerConfig struct {
		serversConfig                         []*connection.ServerConfig
		incomingBlockForSignatureVerification <-chan *protoblocktx.Block
		outgoingBlockWithValidTxs             chan<- *protoblocktx.Block
		outgoingBlockWithInvalidTxs           chan<- *protoblocktx.Block
		metrics                               *perfMetrics
	}
)

const (
	txStatusUnknown txValidityStatus = iota
	txStatusValid
	txStatusInvalid
)

func newSignatureVerifierManager(ctx context.Context, config *signVerifierManagerConfig) *signatureVerifierManager {
	ctx, cancelFunc := context.WithCancel(ctx)
	return &signatureVerifierManager{
		config:     config,
		ctx:        ctx,
		cancelFunc: cancelFunc,
	}
}

func (svm *signatureVerifierManager) start() error {
	c := svm.config
	logger.Infof("Connections to %d sv's will be opened from sv manager", len(c.serversConfig))
	svm.signVerifier = make([]*signatureVerifier, len(c.serversConfig))

	perVerifierBufferSizeForOutputBlock := (cap(c.outgoingBlockWithValidTxs) +
		cap(c.outgoingBlockWithInvalidTxs)) / len(c.serversConfig)

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("sv manager creates client to sv [%d] listening on %s", i, serverConfig.Endpoint.String())
		sv, err := newSignatureVerifier(
			svm.ctx,
			c,
			serverConfig,
			perVerifierBufferSizeForOutputBlock,
		)
		if err != nil {
			return err
		}

		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)

		go sv.sendTransactionsAndReceiveStatus()
		go sv.forwardValidatedTransactions(c.outgoingBlockWithValidTxs, c.outgoingBlockWithInvalidTxs)
	}

	return nil
}

func (svm *signatureVerifierManager) setVerificationKey(key *protosigverifierservice.Key) error {
	for i, sv := range svm.signVerifier {
		logger.Debugf("Setting verification key to sv [%d]", i)
		_, err := sv.client.SetVerificationKey(sv.ctx, key)
		logger.Debugf("Verification key successfully set")
		if err != nil {
			return err
		}
	}

	return nil
}

func (svm *signatureVerifierManager) close() {
	logger.Infof("Closing %d connections to sv's", len(svm.signVerifier))
	// This also cancels the stream.
	svm.cancelFunc()
}

func (svm *signatureVerifierManager) done() <-chan struct{} {
	return svm.ctx.Done()
}

func newSignatureVerifier(
	ctx context.Context,
	config *signVerifierManagerConfig,
	serverConfig *connection.ServerConfig,
	outputBlockBufferSize int,
) (*signatureVerifier, error) {
	conn, err := connection.Connect(connection.NewDialConfig(serverConfig.Endpoint))
	if err != nil {
		return nil, err
	}

	return &signatureVerifier{
		ctx:                                   ctx,
		conn:                                  conn,
		client:                                protosigverifierservice.NewVerifierClient(conn),
		resultAccumulator:                     &sync.Map{},
		incomingBlockForSignatureVerification: config.incomingBlockForSignatureVerification,
		validatedBlock:                        make(chan *blockWithResult, outputBlockBufferSize),
		metrics:                               config.metrics,
	}, nil
}

// waitForConnection returns when SV is connected or the SVM have closed.
func (sv *signatureVerifier) waitForConnection() {
	if sv.conn.GetState() == connectivity.Ready {
		return
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-sv.ctx.Done():
			return
		case <-ticker.C:
			logger.Debug("Reconnecting SV.")
			sv.conn.Connect()
			if sv.conn.GetState() == connectivity.Ready {
				return
			}
			logger.Debug("SV connection is not ready.")
		}
	}
}

// sendTransactionsAndReceiveStatus initiates a stream to a verifier
// and use it to send transactions, and receive the results.
// It reconnects the stream in case of failure.
// It stops only when the context was cancelled, i.e., the SVM have closed.
func (sv *signatureVerifier) sendTransactionsAndReceiveStatus() {
	defer close(sv.validatedBlock)

	for sv.ctx.Err() == nil {
		sv.waitForConnection()
		stream, err := sv.client.StartStream(sv.ctx)
		if err != nil {
			logger.Warnf("Failed starting stream with error: %s", err)
			continue
		}
		logger.Debugf("SV stream is connected")

		go sv.receiveTransactionsStatusFromSVService(stream)
		sv.sendTransactionsToSVService(stream)
	}
}

func (sv *signatureVerifier) sendTransactionsToSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
) {
	// Re-send previous transactions
	sv.resultAccumulator.Range(func(key, value any) bool {
		blkWithResult, _ := value.(*blockWithResult) // nolint:revive
		logger.Debugf("Recovering block: %v", key)
		return sendRequest(stream, blkWithResult)
	})

	for {
		var block *protoblocktx.Block
		var ok bool
		select {
		case <-stream.Context().Done():
			// The stream ended or the SVM was closed.
			return
		case block, ok = <-sv.incomingBlockForSignatureVerification:
			if !ok {
				return
			}
		}

		blockSize := len(block.Txs)
		logger.Debugf("New block (size: %d) came from coordinator to sv manager", blockSize)
		blkWithResult := &blockWithResult{
			block:              block,
			pendingResultCount: blockSize,
			txStatus:           make([]txValidityStatus, blockSize),
		}
		sv.resultAccumulator.Store(block.Number, blkWithResult)
		logger.Debugf("Block contains %d TXs was stored in the accumulator", blockSize)

		if !sendRequest(stream, blkWithResult) {
			// The stream ended or the SVM was closed.
			return
		}
		logger.Debugf("Block contains %d TXs, and was stored in the accumulator and sent to a sv", blockSize)
	}
}

func sendRequest(
	stream protosigverifierservice.Verifier_StartStreamClient,
	blkWithResult *blockWithResult,
) bool {
	if err := stream.Send(makeRequest(blkWithResult)); err != nil {
		logger.Warnf("Send to stream ended with error: %s. Reconnecting.", err)
		return false
	}
	return true
}

func makeRequest(blkWithResult *blockWithResult) *protosigverifierservice.RequestBatch {
	request := &protosigverifierservice.RequestBatch{
		Requests: make([]*protosigverifierservice.Request, 0, len(blkWithResult.block.Txs)),
	}
	for txNum, tx := range blkWithResult.block.Txs {
		if blkWithResult.txStatus[txNum] != txStatusUnknown {
			continue
		}
		request.Requests = append(request.Requests, &protosigverifierservice.Request{
			BlockNum: blkWithResult.block.Number,
			TxNum:    uint64(txNum),
			Tx:       tx,
		})
	}
	return request
}

func (sv *signatureVerifier) receiveTransactionsStatusFromSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
) {
	for {
		// stream.Recv() is using `sv.ctx`, so it will unblock once the context ended.
		response, err := stream.Recv()
		if err != nil {
			// The stream ended or the SVM was closed.
			logger.Warnf("Receive from stream ended with error: %s. Reconnecting.", err)
			return
		}

		logger.Debugf("New batch came from sv to sv manager, contains %d items", len(response.Responses))
		var blkWithResult *blockWithResult
		for _, resp := range response.Responses {
			blkWithResult = sv.updateBlockWithResult(resp, blkWithResult)
			if blkWithResult.pendingResultCount == 0 {
				logger.Debugf("Block [%d] is now fully validated", blkWithResult.block.Number)
				sv.resultAccumulator.Delete(blkWithResult.block.Number)
				sv.validatedBlock <- blkWithResult
			}
		}
	}
}

// updateBlockWithResult the appropriate block from the result accumulator according to the received response.
// If the received blkWithResult matches the current block, it will use it instead of loading it
// from the accumulator.
// Returns the relevant block.
func (sv *signatureVerifier) updateBlockWithResult(
	resp *protosigverifierservice.Response, blkWithResult *blockWithResult,
) *blockWithResult {
	if blkWithResult == nil || blkWithResult.block.Number != resp.BlockNum {
		v, ok := sv.resultAccumulator.Load(resp.BlockNum)
		if !ok {
			// This should not happen if the verifier dismisses all jobs when the stream is closed, as it should.
			logger.Debugf("Received results for unexpected block number [%d]", blkWithResult.block.Number)
			return blkWithResult
		}
		blkWithResult, _ = v.(*blockWithResult) // nolint:revive
	}

	txIndex := int(resp.TxNum)
	// Due to resubmit, we might receive the same TX twice. We can ignore it.
	if blkWithResult.txStatus[txIndex] != txStatusUnknown {
		return blkWithResult
	}

	if resp.GetIsValid() {
		blkWithResult.validTxCount++
		blkWithResult.txStatus[txIndex] = txStatusValid
	} else {
		blkWithResult.invalidTxCount++
		blkWithResult.txStatus[txIndex] = txStatusInvalid
	}
	blkWithResult.pendingResultCount--
	return blkWithResult
}

func (sv *signatureVerifier) forwardValidatedTransactions(
	outgoingBlockWithValidTxs, outgoingBlockWithInvalidTxs chan<- *protoblocktx.Block,
) {
	var blkWithResult *blockWithResult
	var ok bool
	for {
		select {
		case <-sv.ctx.Done():
			return
		case blkWithResult, ok = <-sv.validatedBlock:
			if !ok {
				return
			}
		}

		logger.Debugf("Validated block [%d] contains %d valid and %d invalid TXs",
			blkWithResult.block.Number, blkWithResult.validTxCount, blkWithResult.invalidTxCount)
		sv.metrics.addToCounter(
			sv.metrics.sigverifierTransactionProcessedTotal,
			len(blkWithResult.block.Txs),
		)

		validBlockTxs, invalidBlockTxs := blkWithResult.prepareOutgoingBlock()
		outgoingBlockWithValidTxs <- validBlockTxs
		if invalidBlockTxs != nil {
			outgoingBlockWithInvalidTxs <- invalidBlockTxs
		}
		logger.Debugf("Forwarded valid and invalid TXs of block [%d] back to coordinator",
			blkWithResult.block.Number)
	}
}

func (b *blockWithResult) prepareOutgoingBlock() (validBlockTxs, invalidBlockTxs *protoblocktx.Block) {
	blockNum := b.block.Number
	switch {
	// No invalid TXs.
	case b.invalidTxCount == 0:
		validBlockTxs = b.block
		invalidBlockTxs = nil
	// No valid TXs.
	case b.validTxCount == 0:
		validBlockTxs = &protoblocktx.Block{Number: blockNum}
		invalidBlockTxs = b.block
	// Both valid and invalid TXs.
	default:
		validBlockTxs = &protoblocktx.Block{
			Number: blockNum,
			Txs:    make([]*protoblocktx.Tx, 0, b.validTxCount),
		}
		invalidBlockTxs = &protoblocktx.Block{
			Number: blockNum,
			Txs:    make([]*protoblocktx.Tx, 0, b.invalidTxCount),
		}

		for txNum, tx := range b.block.Txs {
			if b.txStatus[txNum] == txStatusValid {
				validBlockTxs.Txs = append(validBlockTxs.Txs, tx)
			} else {
				invalidBlockTxs.Txs = append(invalidBlockTxs.Txs, tx)
			}
		}
	}
	return validBlockTxs, invalidBlockTxs
}
