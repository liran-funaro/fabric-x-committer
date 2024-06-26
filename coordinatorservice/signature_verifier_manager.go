package coordinatorservice

import (
	"context"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
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

		metrics *perfMetrics
	}

	// signatureVerifier is responsible for managing the communication with a single
	// signature verifier server.
	signatureVerifier struct {
		ctx    context.Context
		conn   *grpc.ClientConn
		client protosigverifierservice.VerifierClient

		// resultAccumulator is used to accumulate the result of the transactions
		// at block level from the signature verifier server.
		// The key is the block number and the value is the blockWithResult.
		// This accumulator is needed because the signature verifier server
		// does not guarantee the order of the responses.
		resultAccumulator *sync.Map
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
		metrics:    config.metrics,
	}
}

func (svm *signatureVerifierManager) start() error {
	c := svm.config
	logger.Infof("Connections to %d sv's will be opened from sv manager", len(c.serversConfig))
	svm.signVerifier = make([]*signatureVerifier, len(c.serversConfig))

	internalQueueSize := cap(c.outgoingBlockWithValidTxs) + cap(c.outgoingBlockWithInvalidTxs)
	blocksQueue := channel.NewReaderWriter(svm.ctx, make(chan *blockWithResult, internalQueueSize))
	go ingestBlocks(
		channel.NewReader(svm.ctx, c.incomingBlockForSignatureVerification),
		blocksQueue,
	)
	validateBlocksQueue := channel.NewReaderWriter(svm.ctx, make(chan *blockWithResult, internalQueueSize))
	go svm.forwardValidatedTransactions(
		validateBlocksQueue,
		channel.NewWriter(svm.ctx, c.outgoingBlockWithValidTxs),
		channel.NewWriter(svm.ctx, c.outgoingBlockWithInvalidTxs),
	)

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("sv manager creates client to sv [%d] listening on %s", i, serverConfig.Endpoint.String())
		sv, err := newSignatureVerifier(svm.ctx, serverConfig)
		if err != nil {
			return err
		}

		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)
		go sv.sendTransactionsAndReceiveStatus(blocksQueue, validateBlocksQueue)
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

func ingestBlocks(
	incomingBlocks channel.Reader[protoblocktx.Block],
	blocksQueue channel.Writer[blockWithResult],
) {
	for block, ok := incomingBlocks.Read(); ok; block, ok = incomingBlocks.Read() {
		blockSize := len(block.Txs)
		logger.Debugf("New block (size: %d) came from coordinator to sv manager", blockSize)
		blocksQueue.Write(&blockWithResult{
			block:              block,
			pendingResultCount: blockSize,
			txStatus:           make([]txValidityStatus, blockSize),
		})
	}
}

func (svm *signatureVerifierManager) forwardValidatedTransactions(
	validated channel.Reader[blockWithResult],
	outgoingBlockWithValidTxs, outgoingBlockWithInvalidTxs channel.Writer[protoblocktx.Block],
) {
	for blkWithResult, ok := validated.Read(); ok; blkWithResult, ok = validated.Read() {
		logger.Debugf("Validated block [%d] contains %d valid and %d invalid TXs",
			blkWithResult.block.Number, blkWithResult.validTxCount, blkWithResult.invalidTxCount)
		svm.metrics.addToCounter(
			svm.metrics.sigverifierTransactionProcessedTotal,
			len(blkWithResult.block.Txs),
		)

		validBlockTxs, invalidBlockTxs := blkWithResult.prepareOutgoingBlock()
		outgoingBlockWithValidTxs.Write(validBlockTxs)
		if invalidBlockTxs != nil {
			outgoingBlockWithInvalidTxs.Write(invalidBlockTxs)
		}
		logger.Debugf("Forwarded valid and invalid TXs of block [%d] back to coordinator",
			blkWithResult.block.Number)
	}
}

func newSignatureVerifier(
	ctx context.Context,
	serverConfig *connection.ServerConfig,
) (*signatureVerifier, error) {
	conn, err := connection.Connect(connection.NewDialConfig(serverConfig.Endpoint))
	if err != nil {
		return nil, err
	}

	return &signatureVerifier{
		ctx:               ctx,
		conn:              conn,
		client:            protosigverifierservice.NewVerifierClient(conn),
		resultAccumulator: &sync.Map{},
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
func (sv *signatureVerifier) sendTransactionsAndReceiveStatus(
	inputBlocks channel.ReaderWriter[blockWithResult],
	validatedBlocks channel.Writer[blockWithResult],
) {
	for sv.ctx.Err() == nil {
		// Re-enter previous blocks to the queue so other workers can fetch them.
		sv.resultAccumulator.Range(func(key, value any) bool {
			blkWithResult, _ := value.(*blockWithResult) // nolint:revive
			logger.Debugf("Recovering block: %v", key)
			return inputBlocks.Write(blkWithResult)
		})
		sv.resultAccumulator = &sync.Map{}

		sv.waitForConnection()
		stream, err := sv.client.StartStream(sv.ctx)
		if err != nil {
			logger.Warnf("Failed starting stream with error: %s", err)
			continue
		}
		logger.Debugf("SV stream is connected")

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			sv.receiveTransactionsStatusFromSVService(stream, validatedBlocks)
		}()
		sv.sendTransactionsToSVService(stream, inputBlocks)
		wg.Wait()
		logger.Debug("Stream ended")
	}
}

func (sv *signatureVerifier) sendTransactionsToSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
	inputBlocks channel.Writer[blockWithResult],
) {
	// This make sure we exit immediately when the stream ends.
	streamInputBlocks := inputBlocks.WithContext(stream.Context())
	for blkWithResult, ok := streamInputBlocks.Read(); ok; blkWithResult, ok = streamInputBlocks.Read() {
		blockSize := len(blkWithResult.block.Txs)
		sv.resultAccumulator.Store(blkWithResult.block.Number, blkWithResult)
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
	validatedBlocks channel.Writer[blockWithResult],
) {
	// This make sure we exit immediately when the stream ends.
	streamValidatedBlocks := validatedBlocks.WithContext(stream.Context())
	for stream.Context().Err() == nil {
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
				if ok := streamValidatedBlocks.Write(blkWithResult); !ok {
					return
				}
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
