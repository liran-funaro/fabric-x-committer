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
		serversConfig                         []*connection.ServerConfig
		signVerifier                          []*signatureVerifier
		incomingBlockForSignatureVerification <-chan *protoblocktx.Block
		outgoingBlockWithValidTxs             chan<- *protoblocktx.Block
		outgoingBlockWithInvalidTxs           chan<- *protoblocktx.Block
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
	}
)

func newSignatureVerifierManager(config *signVerifierManagerConfig) *signatureVerifierManager {
	return &signatureVerifierManager{
		serversConfig:                         config.serversConfig,
		incomingBlockForSignatureVerification: config.incomingBlockForSignatureVerification,
		outgoingBlockWithValidTxs:             config.outgoingBlockWithValidTxs,
		outgoingBlockWithInvalidTxs:           config.outgoingBlockWithInvalidTxs,
	}
}

func (svm *signatureVerifierManager) start() (chan error, error) {
	svm.signVerifier = make([]*signatureVerifier, len(svm.serversConfig))

	numErrorableGoroutinePerServer := 2
	errChan := make(chan error, numErrorableGoroutinePerServer*len(svm.serversConfig))

	for i, serverConfig := range svm.serversConfig {
		conn, err := connection.Connect(connection.NewDialConfig(serverConfig.Endpoint))
		if err != nil {
			return nil, err
		}

		client := protosigverifierservice.NewVerifierClient(conn)
		vcStream, err := client.StartStream(context.Background())
		if err != nil {
			return nil, err
		}

		sv := &signatureVerifier{
			client:            client,
			stream:            vcStream,
			resultAccumulator: &sync.Map{},
			responseCollectionChan: make(
				chan *protosigverifierservice.ResponseBatch,
				cap(svm.incomingBlockForSignatureVerification)/len(svm.serversConfig),
			),
			validatedBlock: make(
				chan *blockWithResult,
				(cap(svm.outgoingBlockWithValidTxs)+cap(svm.outgoingBlockWithInvalidTxs))/len(svm.serversConfig),
			),
		}
		svm.signVerifier[i] = sv

		go func() {
			errChan <- sv.sendTransactionsToSVService(svm.incomingBlockForSignatureVerification)
		}()
		go func() {
			errChan <- sv.receiveTransactionsStatusFromSVService()
		}()
		go func() {
			sv.processTransactionStatus()
		}()
		go func() {
			sv.forwardValidatedTransactions(svm.outgoingBlockWithValidTxs, svm.outgoingBlockWithInvalidTxs)
		}()
	}

	return errChan, nil
}

func (svm *signatureVerifierManager) setVerificationKey(key *protosigverifierservice.Key) error {
	for _, sv := range svm.signVerifier {
		_, err := sv.client.SetVerificationKey(context.Background(), key)
		if err != nil {
			return err
		}
	}

	return nil
}

func (svm *signatureVerifierManager) close() error {
	for _, sv := range svm.signVerifier {
		close(sv.responseCollectionChan)
		close(sv.validatedBlock)
		if err := sv.stream.CloseSend(); err != nil {
			return err
		}
	}

	return nil
}

func (sv *signatureVerifier) sendTransactionsToSVService(inputBlock <-chan *protoblocktx.Block) error {
	for block := range inputBlock {
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
				Tx:       nil,
			})
		}

		if err := sv.stream.Send(r); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}

	return nil
}

func (sv *signatureVerifier) receiveTransactionsStatusFromSVService() error {
	for {
		response, err := sv.stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		sv.responseCollectionChan <- response
	}
}

func (sv *signatureVerifier) processTransactionStatus() {
	for r := range sv.responseCollectionChan {
		var blkWithResult *blockWithResult
		for _, resp := range r.Responses {
			if blkWithResult == nil || blkWithResult.block.Number != resp.BlockNum {
				v, _ := sv.resultAccumulator.Load(resp.BlockNum)
				blkWithResult, _ = v.(*blockWithResult)
			}

			switch resp.GetIsValid() {
			case true:
				blkWithResult.validTxIndex = append(blkWithResult.validTxIndex, int(resp.TxNum))
			default:
				blkWithResult.invalidTxIndex = append(blkWithResult.invalidTxIndex, int(resp.TxNum))
			}
			blkWithResult.pendingResults--

			if blkWithResult.pendingResults == 0 {
				sv.resultAccumulator.Delete(blkWithResult.block.Number)
				sv.validatedBlock <- blkWithResult
			}
		}
	}
}

func (sv *signatureVerifier) forwardValidatedTransactions(
	outgoingBlockWithValidTxs, outgoingBlockWithInvalidTxs chan<- *protoblocktx.Block,
) {
	for blkWithResult := range sv.validatedBlock {
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
		}
	}
}
