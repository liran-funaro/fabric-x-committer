package sigverifiermock

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
)

// MockSigVerifier is a mock implementation of the protosignverifierservice.VerifierServer.
// MockSigVerifier marks valid and invalid flag as follows:
// - when the block number is even, the even numbered txs are valid and the odd numbered txs are invalid.
// - when the block number is odd, the even numbered txs are invalid and the odd numbered txs are valid.
type MockSigVerifier struct {
	protosigverifierservice.UnimplementedVerifierServer
	requestBatch      chan *protosigverifierservice.RequestBatch
	verificationKey   []byte
	numBlocksReceived *atomic.Uint32
}

// NewMockSigVerifier returns a new mock verifier.
func NewMockSigVerifier() *MockSigVerifier {
	return &MockSigVerifier{
		UnimplementedVerifierServer: protosigverifierservice.UnimplementedVerifierServer{},
		requestBatch:                make(chan *protosigverifierservice.RequestBatch, 10),
		numBlocksReceived:           &atomic.Uint32{},
	}
}

// SetVerificationKey is a mock implementation of the protosignverifierservice.VerifierServer.
func (m *MockSigVerifier) SetVerificationKey(
	_ context.Context,
	k *protosigverifierservice.Key,
) (*protosigverifierservice.Empty, error) {
	m.verificationKey = k.SerializedBytes
	return &protosigverifierservice.Empty{}, nil
}

// StartStream is a mock implementation of the protosignverifierservice.VerifierServer.
func (m *MockSigVerifier) StartStream(stream protosigverifierservice.Verifier_StartStreamServer) error {
	errChan := make(chan error, 2)

	go func() {
		errChan <- m.receiveRequestBatch(stream)
	}()

	go func() {
		errChan <- m.sendResponseBatch(stream)
	}()

	for i := 0; i < 2; i++ {
		err := <-errChan
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *MockSigVerifier) receiveRequestBatch(stream protosigverifierservice.Verifier_StartStreamServer) error {
	for {
		reqBatch, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		m.requestBatch <- reqBatch
		m.numBlocksReceived.Add(1)
	}
}

func (m *MockSigVerifier) sendResponseBatch(stream protosigverifierservice.Verifier_StartStreamServer) error {
	for reqBatch := range m.requestBatch {
		respBatch := &protosigverifierservice.ResponseBatch{
			Responses: make([]*protosigverifierservice.Response, len(reqBatch.Requests)),
		}

		for i, req := range reqBatch.Requests {
			resp := &protosigverifierservice.Response{
				BlockNum: req.BlockNum,
				TxNum:    req.TxNum,
			}

			switch req.BlockNum % 2 {
			case 0:
				// for even block numbers, even tx numbers are valid and odd tx numbers are invalid
				resp.IsValid = i%2 == 0
			case 1:
				// for odd block numbers, even tx numbers are invalid and odd tx numbers are valid
				resp.IsValid = i%2 == 1
			}
			respBatch.Responses[i] = resp
		}

		if err := stream.Send(respBatch); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}

	return nil
}

// GetNumBlocksReceived returns the number of blocks received by the mock verifier.
func (m *MockSigVerifier) GetNumBlocksReceived() uint32 {
	return m.numBlocksReceived.Load()
}

// GetVerificationKey returns the verification key of the mock verifier.
func (m *MockSigVerifier) GetVerificationKey() []byte {
	return m.verificationKey
}

// Close closes the mock verifier.
func (m *MockSigVerifier) Close() {
	close(m.requestBatch)
}
