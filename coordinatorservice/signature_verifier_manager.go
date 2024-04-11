package coordinatorservice

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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
	}

	// signatureVerifier is responsible for managing the communication with a single
	// signature verifier server.
	signatureVerifier struct {
		client                 protosigverifierservice.VerifierClient
		stream                 protosigverifierservice.Verifier_StartStreamClient
		responseCollectionChan chan *protosigverifierservice.ResponseBatch

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

		// stopGoroutines is used to stopGoroutines goroutines created by the signatureVerifier.
		stopGoroutines chan any
		// wg is used to ensure that all goroutines in the signature verifier manager
		// are terminated before closing it.
		wg sync.WaitGroup
	}

	blockWithResult struct {
		block          *protoblocktx.Block
		validTxIndex   []int
		invalidTxIndex []int
		pendingResults int
	}

	signVerifierManagerConfig struct {
		serversConfig                         []*connection.ServerConfig
		incomingBlockForSignatureVerification <-chan *protoblocktx.Block
		outgoingBlockWithValidTxs             chan<- *protoblocktx.Block
		outgoingBlockWithInvalidTxs           chan<- *protoblocktx.Block
		metrics                               *perfMetrics
	}
)

func newSignatureVerifierManager(config *signVerifierManagerConfig) *signatureVerifierManager {
	return &signatureVerifierManager{
		config: config,
	}
}

func (svm *signatureVerifierManager) start() (chan error, error) {
	c := svm.config
	logger.Infof("Connections to %d sv's will be opened from sv manager", len(c.serversConfig))
	svm.signVerifier = make([]*signatureVerifier, len(c.serversConfig))

	numErrorableGoroutinePerServer := 2
	errChan := make(chan error, numErrorableGoroutinePerServer*len(c.serversConfig))

	perVerifierBufferSizeForInputBlock := cap(c.incomingBlockForSignatureVerification) / len(c.serversConfig)
	perVerifierBufferSizeForOutputBlock := (cap(c.outgoingBlockWithValidTxs) +
		cap(c.outgoingBlockWithInvalidTxs)) / len(c.serversConfig)

	for i, serverConfig := range c.serversConfig {
		logger.Debugf("sv manager creates client to sv [%d] listening on %s", i, serverConfig.Endpoint.String())
		sv, err := newSignatureVerifier(
			serverConfig,
			perVerifierBufferSizeForInputBlock,
			perVerifierBufferSizeForOutputBlock,
			svm.config.metrics,
		)
		if err != nil {
			return nil, err
		}

		svm.signVerifier[i] = sv
		logger.Debugf("Client [%d] successfully created and connected to sv", i)

		sv.wg.Add(4)
		go func() {
			defer sv.wg.Done()
			errChan <- sv.sendTransactionsToSVService(c.incomingBlockForSignatureVerification)
		}()
		go func() {
			defer sv.wg.Done()
			errChan <- sv.receiveTransactionsStatusFromSVService()
		}()
		go func() {
			defer sv.wg.Done()
			sv.processTransactionStatus()
		}()
		go func() {
			defer sv.wg.Done()
			sv.forwardValidatedTransactions(c.outgoingBlockWithValidTxs, c.outgoingBlockWithInvalidTxs)
		}()
	}

	return errChan, nil
}

func (svm *signatureVerifierManager) setVerificationKey(key *protosigverifierservice.Key) error {
	for i, sv := range svm.signVerifier {
		logger.Debugf("Setting verification key to sv [%d]", i)
		_, err := sv.client.SetVerificationKey(context.Background(), key)
		logger.Debugf("Verification key successfully set")
		if err != nil {
			return err
		}
	}

	return nil
}

func (svm *signatureVerifierManager) close() error {
	logger.Infof("Closing %d connections to sv's", len(svm.signVerifier))
	for _, sv := range svm.signVerifier {
		if err := sv.close(); err != nil {
			return err
		}
	}

	return nil
}

func newSignatureVerifier(
	serverConfig *connection.ServerConfig,
	inputBlockBufferSize, outputBlockBufferSize int,
	m *perfMetrics,
) (*signatureVerifier, error) {
	conn, err := connection.Connect(connection.NewDialConfig(serverConfig.Endpoint))
	if err != nil {
		return nil, err
	}

	client := protosigverifierservice.NewVerifierClient(conn)
	vcStream, err := client.StartStream(context.Background())
	if err != nil {
		return nil, err
	}

	return &signatureVerifier{
		client:                 client,
		stream:                 vcStream,
		resultAccumulator:      &sync.Map{},
		responseCollectionChan: make(chan *protosigverifierservice.ResponseBatch, inputBlockBufferSize),
		validatedBlock:         make(chan *blockWithResult, outputBlockBufferSize),
		metrics:                m,
		stopGoroutines:         make(chan any),
	}, nil
}

func (sv *signatureVerifier) sendTransactionsToSVService( //nolint:gocognit
	inputBlock <-chan *protoblocktx.Block,
) error {
	var block *protoblocktx.Block
	var ok bool
	for {
		select {
		case <-sv.stopGoroutines:
			return nil
		case block, ok = <-inputBlock:
			if !ok {
				return nil
			}
		}

		logger.Debugf("New block came from coordinator to sv manager")
		sv.resultAccumulator.Store(
			block.Number,
			&blockWithResult{
				block:          block,
				pendingResults: len(block.Txs),
			},
		)

		r := &protosigverifierservice.RequestBatch{}
		for txNum := range block.Txs {
			r.Requests = append(r.Requests, &protosigverifierservice.Request{
				BlockNum: block.Number,
				TxNum:    uint64(txNum),
				Tx:       block.Txs[txNum],
			})
		}

		if err := sv.stream.Send(r); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		logger.Debugf("Block contains %d TXs, and was stored in the accumulator and sent to a sv", len(block.Txs))
	}
}

func (sv *signatureVerifier) receiveTransactionsStatusFromSVService() error {
	response := make(chan *protosigverifierservice.ResponseBatch)
	streamErr := make(chan error)

	// As the stream.Recv() is blocking, we need to run it in a separate goroutine.
	// Thus, we can stop the goroutine when the stop channel is closed.
	go func() {
		for {
			r, err := sv.stream.Recv()
			if err != nil {
				streamErr <- err
				return
			}
			response <- r
		}
	}()

	for {
		select {
		case <-sv.stopGoroutines:
			return nil
		case err := <-streamErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case response, ok := <-response:
			if !ok {
				return nil
			}
			sv.responseCollectionChan <- response
		}
	}
}

func (sv *signatureVerifier) processTransactionStatus() { // nolint:gocognit
	var response *protosigverifierservice.ResponseBatch
	var ok bool
	for {
		select {
		case <-sv.stopGoroutines:
			return
		case response, ok = <-sv.responseCollectionChan:
			if !ok {
				return
			}
		}

		logger.Debugf("Batch contains %d items in total", len(response.Responses))
		var blkWithResult *blockWithResult
		for _, resp := range response.Responses {
			if blkWithResult == nil || blkWithResult.block.Number != resp.BlockNum {
				v, _ := sv.resultAccumulator.Load(resp.BlockNum)
				blkWithResult, _ = v.(*blockWithResult) // nolint:revive
			}

			switch resp.GetIsValid() {
			case true:
				blkWithResult.validTxIndex = append(blkWithResult.validTxIndex, int(resp.TxNum))
			default:
				blkWithResult.invalidTxIndex = append(blkWithResult.invalidTxIndex, int(resp.TxNum))
			}
			blkWithResult.pendingResults--

			if blkWithResult.pendingResults != 0 {
				continue
			}
			logger.Debugf("Block [%d] is now fully validated", blkWithResult.block.Number)
			sv.resultAccumulator.Delete(blkWithResult.block.Number)
			sv.validatedBlock <- blkWithResult
		}
	}
}

func (sv *signatureVerifier) forwardValidatedTransactions( // nolint:gocognit
	outgoingBlockWithValidTxs, outgoingBlockWithInvalidTxs chan<- *protoblocktx.Block,
) {
	var blkWithResult *blockWithResult
	var ok bool
	for {
		select {
		case <-sv.stopGoroutines:
			return
		case blkWithResult, ok = <-sv.validatedBlock:
			if !ok {
				return
			}
		}

		logger.Debugf("Validated block [%d] contains %d valid and %d invalid TXs",
			blkWithResult.block.Number, len(blkWithResult.validTxIndex), len(blkWithResult.invalidTxIndex))
		sv.metrics.addToCounter(
			sv.metrics.sigverifierTransactionProcessedTotal,
			len(blkWithResult.block.Txs),
		)

		switch {
		case len(blkWithResult.invalidTxIndex) == 0:
			outgoingBlockWithValidTxs <- blkWithResult.block
			continue
		case len(blkWithResult.validTxIndex) == 0:
			outgoingBlockWithInvalidTxs <- blkWithResult.block
			outgoingBlockWithValidTxs <- &protoblocktx.Block{
				Number: blkWithResult.block.Number,
			}
			continue
		default:
			validBlockTxs := &protoblocktx.Block{
				Number: blkWithResult.block.Number,
			}
			invalidBlockTxs := &protoblocktx.Block{
				Number: blkWithResult.block.Number,
			}

			for _, txNum := range blkWithResult.validTxIndex {
				validBlockTxs.Txs = append(validBlockTxs.Txs, blkWithResult.block.Txs[txNum])
			}
			for _, txNum := range blkWithResult.invalidTxIndex {
				invalidBlockTxs.Txs = append(invalidBlockTxs.Txs, blkWithResult.block.Txs[txNum])
			}
			outgoingBlockWithValidTxs <- validBlockTxs
			outgoingBlockWithInvalidTxs <- invalidBlockTxs
			logger.Debugf("Forwarded valid and invalid TXs of block [%d] back to coordinator",
				blkWithResult.block.Number)
		}
	}
}

func (sv *signatureVerifier) close() error {
	close(sv.stopGoroutines)
	sv.wg.Wait()
	close(sv.responseCollectionChan)
	close(sv.validatedBlock)
	return sv.stream.CloseSend()
}
