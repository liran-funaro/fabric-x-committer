package coordinatorservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
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

	internalQueueSize := cap(c.outgoingBlockWithValidTxs) + cap(c.outgoingBlockWithInvalidTxs)
	blocksQueue := channel.NewReaderWriter(eCtx, make(chan *blockWithResult, internalQueueSize))
	g.Go(func() error {
		ingestBlocks(
			channel.NewReader(eCtx, c.incomingBlockForSignatureVerification),
			blocksQueue,
		)
		return nil
	})

	validateBlocksQueue := channel.NewReaderWriter(eCtx, make(chan *blockWithResult, internalQueueSize))
	g.Go(func() error {
		svm.forwardValidatedTransactions(
			validateBlocksQueue,
			channel.NewWriter(eCtx, c.outgoingBlockWithValidTxs),
			channel.NewWriter(eCtx, c.outgoingBlockWithInvalidTxs),
		)
		return nil
	})

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("sv manager creates client to sv [%d] listening on %s", i, &serverConfig.Endpoint)
		sv, err := newSignatureVerifier(serverConfig)
		if err != nil {
			return err
		}

		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)
		g.Go(func() error {
			sv.sendTransactionsAndReceiveStatus(eCtx, blocksQueue, validateBlocksQueue)
			return nil
		})
	}

	close(svm.connectionReady)
	return g.Wait()
}

func (svm *signatureVerifierManager) setVerificationKey(ctx context.Context, key *protosigverifierservice.Key) error {
	for i, sv := range svm.signVerifier {
		logger.Debugf("Setting verification key to sv [%d]", i)
		_, err := sv.client.SetVerificationKey(ctx, key)
		logger.Debugf("Verification key successfully set")
		if err != nil {
			return err
		}
	}

	return nil
}

func ingestBlocks(
	incomingBlocks channel.Reader[*protoblocktx.Block],
	blocksQueue channel.Writer[*blockWithResult],
) {
	for {
		block, ctxAlive := incomingBlocks.Read()
		if !ctxAlive {
			return
		}

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
	validated channel.Reader[*blockWithResult],
	outgoingBlockWithValidTxs, outgoingBlockWithInvalidTxs channel.Writer[*protoblocktx.Block],
) {
	for {
		blkWithResult, ctxAlive := validated.Read()
		if !ctxAlive {
			return
		}
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

func newSignatureVerifier(serverConfig *connection.ServerConfig) (*signatureVerifier, error) {
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
		conn:              conn,
		client:            protosigverifierservice.NewVerifierClient(conn),
		resultAccumulator: &sync.Map{},
	}, nil
}

// sendTransactionsAndReceiveStatus initiates a stream to a verifier
// and use it to send transactions, and receive the results.
// It reconnects the stream in case of failure.
// It stops only when the context was cancelled, i.e., the SVM have closed.
func (sv *signatureVerifier) sendTransactionsAndReceiveStatus(
	ctx context.Context,
	inputBlocks channel.ReaderWriter[*blockWithResult],
	validatedBlocks channel.Writer[*blockWithResult],
) {
	for ctx.Err() == nil {
		// Re-enter previous blocks to the queue so other workers can fetch them.
		sv.resultAccumulator.Range(func(key, value any) bool {
			blkWithResult, _ := value.(*blockWithResult) // nolint:revive
			logger.Debugf("Recovering block: %v", key)
			return inputBlocks.Write(blkWithResult)
		})
		sv.resultAccumulator = &sync.Map{}

		sv.waitForConnection(ctx)
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
			// NOTE: validatedBlocks should not use the stream context. Otherwise, these is a
			// possibility of losing a validated block.
			sv.receiveTransactionsStatusFromSVService(stream, validatedBlocks)
		}()
		sv.sendTransactionsToSVService(stream, inputBlocks.WithContext(stream.Context()))
		wg.Wait()
		logger.Debug("Stream ended")
	}
}

// waitForConnection returns when SV is connected or the SVM have closed.
func (sv *signatureVerifier) waitForConnection(ctx context.Context) {
	if sv.conn.GetState() == connectivity.Ready {
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
			sv.conn.Connect()
			if sv.conn.GetState() == connectivity.Ready {
				return
			}
			logger.Debug("SV connection is not ready.")
		}
	}
}

func (sv *signatureVerifier) sendTransactionsToSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
	inputBlocks channel.Reader[*blockWithResult],
) {
	for {
		blkWithResult, ctxAlive := inputBlocks.Read()
		if !ctxAlive {
			return
		}

		blockSize := len(blkWithResult.block.Txs)
		sv.resultAccumulator.Store(blkWithResult.block.Number, blkWithResult)
		logger.Debugf("Block contains %d TXs was stored in the accumulator", blockSize)

		request := &protosigverifierservice.RequestBatch{
			Requests: make([]*protosigverifierservice.Request, len(blkWithResult.block.Txs)),
		}
		for txNum, tx := range blkWithResult.block.Txs {
			if blkWithResult.txStatus[txNum] != txStatusUnknown {
				continue
			}
			request.Requests[txNum] = &protosigverifierservice.Request{
				BlockNum: blkWithResult.block.Number,
				TxNum:    uint64(txNum),
				Tx:       tx,
			}
		}

		if err := stream.Send(request); err != nil {
			logger.Warnf("Send to stream ended with error: %s. Reconnecting.", err)
			return
		}
		logger.Debugf("Block contains %d TXs, and was stored in the accumulator and sent to a sv", blockSize)
	}
}

func (sv *signatureVerifier) receiveTransactionsStatusFromSVService(
	stream protosigverifierservice.Verifier_StartStreamClient,
	validatedBlocks channel.Writer[*blockWithResult],
) {
	for {
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
				if ctxAlive := validatedBlocks.Write(blkWithResult); !ctxAlive {
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

		for i, tx := range b.block.Txs {
			if b.txStatus[i] == txStatusValid {
				validBlockTxs.Txs = append(validBlockTxs.Txs, tx)
				validBlockTxs.TxsNum = append(validBlockTxs.TxsNum, b.block.TxsNum[i])
			} else {
				invalidBlockTxs.Txs = append(invalidBlockTxs.Txs, tx)
				invalidBlockTxs.TxsNum = append(invalidBlockTxs.TxsNum, b.block.TxsNum[i])
			}
		}
	}
	return validBlockTxs, invalidBlockTxs
}
