package mock

import (
	"context"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
)

// SigVerifier is a mock implementation of the protosignverifierservice.VerifierServer.
// SigVerifier marks valid and invalid flag as follows:
// - when the tx has empty signature, it is invalid.
// - when the tx has non-empty signature, it is valid.
type SigVerifier struct {
	protosigverifierservice.UnimplementedVerifierServer
	policies          *protoblocktx.Policies
	numBlocksReceived *atomic.Uint32
	// MockFaultyNodeDropSize allows mocking a faulty node by dropping some TXs.
	MockFaultyNodeDropSize int
	requestBatch           chan *protosigverifierservice.RequestBatch
}

// NewMockSigVerifier returns a new mock verifier.
func NewMockSigVerifier() *SigVerifier {
	return &SigVerifier{
		UnimplementedVerifierServer: protosigverifierservice.UnimplementedVerifierServer{},
		numBlocksReceived:           &atomic.Uint32{},
		policies:                    &protoblocktx.Policies{},
	}
}

// UpdatePolicies is a mock implementation of the protosignverifierservice.UpdatePolicies.
func (m *SigVerifier) UpdatePolicies(_ context.Context, policies *protoblocktx.Policies) (
	*protosigverifierservice.Empty, error,
) {
	m.policies.Policies = append(m.policies.Policies, policies.Policies...)
	logger.Info("policy has been updated")
	return &protosigverifierservice.Empty{}, nil
}

// StartStream is a mock implementation of the protosignverifierservice.VerifierServer.
func (m *SigVerifier) StartStream(stream protosigverifierservice.Verifier_StartStreamServer) error {
	m.requestBatch = make(chan *protosigverifierservice.RequestBatch, 10)
	g, eCtx := errgroup.WithContext(stream.Context())

	g.Go(func() error {
		return m.receiveRequestBatch(eCtx, stream)
	})

	g.Go(func() error {
		return m.sendResponseBatch(eCtx, stream)
	})

	return g.Wait()
}

func (m *SigVerifier) receiveRequestBatch(
	ctx context.Context,
	stream protosigverifierservice.Verifier_StartStreamServer,
) error {
	for ctx.Err() == nil {
		reqBatch, err := stream.Recv()
		if err != nil {
			return connection.FilterStreamRPCError(err)
		}

		logger.Debugf("new batch received at the mock sig verifier with %d requests.", len(reqBatch.Requests))
		m.requestBatch <- reqBatch
		m.numBlocksReceived.Add(1)
	}

	return nil
}

func (m *SigVerifier) sendResponseBatch(
	ctx context.Context,
	stream protosigverifierservice.Verifier_StartStreamServer,
) error {
	for ctx.Err() == nil {
		reqBatch := <-m.requestBatch
		respBatch := &protosigverifierservice.ResponseBatch{
			Responses: make([]*protosigverifierservice.Response, 0, len(reqBatch.Requests)),
		}

		for i, req := range reqBatch.Requests {
			// We simulate a faulty node by not responding to the first X TXs.
			if i < m.MockFaultyNodeDropSize {
				continue
			}
			status := protoblocktx.Status_COMMITTED
			if len(req.GetTx().GetSignatures()) == 0 {
				status = protoblocktx.Status_ABORTED_SIGNATURE_INVALID
			}
			respBatch.Responses = append(respBatch.Responses, &protosigverifierservice.Response{
				BlockNum: req.BlockNum,
				TxNum:    req.TxNum,
				Status:   status,
			})
		}

		if err := stream.Send(respBatch); err != nil {
			return connection.FilterStreamRPCError(err)
		}
	}

	return nil
}

// GetNumBlocksReceived returns the number of blocks received by the mock verifier.
func (m *SigVerifier) GetNumBlocksReceived() uint32 {
	return m.numBlocksReceived.Load()
}

// GetPolicies returns the verification key of the mock verifier.
func (m *SigVerifier) GetPolicies() *protoblocktx.Policies {
	return m.policies
}

// SendRequestBatchWithoutStream allows the caller to bypass the stream to send
// a request batch.
func (m *SigVerifier) SendRequestBatchWithoutStream(requestBatch *protosigverifierservice.RequestBatch) {
	m.requestBatch <- requestBatch
}
