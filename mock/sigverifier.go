package mock

import (
	"context"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

// SigVerifier is a mock implementation of the protosignverifierservice.VerifierServer.
// SigVerifier marks valid and invalid flag as follows:
// - when the tx has empty signature, it is invalid.
// - when the tx has non-empty signature, it is valid.
type SigVerifier struct {
	protosigverifierservice.UnimplementedVerifierServer
	verificationKey   []byte
	numBlocksReceived *atomic.Uint32
	// MockFaultyNodeDropSize allows mocking a faulty node by dropping some TXs.
	MockFaultyNodeDropSize int
}

// NewMockSigVerifier returns a new mock verifier.
func NewMockSigVerifier() *SigVerifier {
	return &SigVerifier{
		UnimplementedVerifierServer: protosigverifierservice.UnimplementedVerifierServer{},
		numBlocksReceived:           &atomic.Uint32{},
	}
}

// SetVerificationKey is a mock implementation of the protosignverifierservice.VerifierServer.
func (m *SigVerifier) SetVerificationKey(
	_ context.Context,
	k *protosigverifierservice.Key,
) (*protosigverifierservice.Empty, error) {
	logger.Info("Verification key has been set")
	m.verificationKey = k.SerializedBytes
	return &protosigverifierservice.Empty{}, nil
}

// StartStream is a mock implementation of the protosignverifierservice.VerifierServer.
func (m *SigVerifier) StartStream(stream protosigverifierservice.Verifier_StartStreamServer) error {
	errChan := make(chan error, 2)
	// We need each stream to have its own requests channel to avoid "stealing" values from other streams.
	requestBatch := make(chan *protosigverifierservice.RequestBatch, 10)

	go func() {
		errChan <- m.receiveRequestBatch(stream, requestBatch)
	}()

	go func() {
		errChan <- m.sendResponseBatch(stream, requestBatch)
	}()

	for i := 0; i < 2; i++ {
		err := <-errChan
		if err != nil {
			return err
		}
	}

	return nil
}

func (m *SigVerifier) receiveRequestBatch(
	stream protosigverifierservice.Verifier_StartStreamServer, requestBatch chan *protosigverifierservice.RequestBatch,
) error {
	for {
		reqBatch, err := stream.Recv()
		if err != nil {
			close(requestBatch)
			return connection.WrapStreamRpcError(err)
		}

		logger.Debugf("new batch received at the mock sig verifier with %d requests.", len(reqBatch.Requests))
		requestBatch <- reqBatch
		m.numBlocksReceived.Add(1)
	}
}

func (m *SigVerifier) sendResponseBatch(
	stream protosigverifierservice.Verifier_StartStreamServer,
	requestBatch <-chan *protosigverifierservice.RequestBatch,
) error {
	for reqBatch := range requestBatch {
		respBatch := &protosigverifierservice.ResponseBatch{
			Responses: make([]*protosigverifierservice.Response, 0, len(reqBatch.Requests)),
		}

		for i, req := range reqBatch.Requests {
			// We simulate a faulty node by not responding to the first X TXs.
			if i < m.MockFaultyNodeDropSize {
				continue
			}
			respBatch.Responses = append(respBatch.Responses, &protosigverifierservice.Response{
				BlockNum: req.BlockNum,
				TxNum:    req.TxNum,
				IsValid:  len(req.GetTx().GetSignatures()) > 0,
			})
		}

		if err := stream.Send(respBatch); err != nil {
			return connection.WrapStreamRpcError(err)
		}
	}

	return nil
}

// GetNumBlocksReceived returns the number of blocks received by the mock verifier.
func (m *SigVerifier) GetNumBlocksReceived() uint32 {
	return m.numBlocksReceived.Load()
}

// GetVerificationKey returns the verification key of the mock verifier.
func (m *SigVerifier) GetVerificationKey() []byte {
	return m.verificationKey
}
