package mock

import (
	"context"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

// SigVerifier is a mock implementation of the protosignverifierservice.VerifierServer.
// SigVerifier marks valid and invalid flag as follows:
// - when the tx has empty signature, it is invalid.
// - when the tx has non-empty signature, it is valid.
type SigVerifier struct {
	protosigverifierservice.UnimplementedVerifierServer
	updates           []*protosigverifierservice.Update
	numBlocksReceived *atomic.Uint32
	// MockFaultyNodeDropSize allows mocking a faulty node by dropping some TXs.
	MockFaultyNodeDropSize     int
	requestBatch               chan *protosigverifierservice.RequestBatch
	returnErrForUpdatePolicies atomic.Bool
	policyUpdateCounter        atomic.Uint64
}

// NewMockSigVerifier returns a new mock verifier.
func NewMockSigVerifier() *SigVerifier {
	return &SigVerifier{
		UnimplementedVerifierServer: protosigverifierservice.UnimplementedVerifierServer{},
		numBlocksReceived:           &atomic.Uint32{},
	}
}

func (m *SigVerifier) updatePolicies(update *protosigverifierservice.Update) error {
	if update == nil {
		return nil
	}
	m.policyUpdateCounter.Add(1)
	if m.returnErrForUpdatePolicies.CompareAndSwap(true, false) {
		return errors.New("invalid argument")
	}
	m.updates = append(m.updates, update)
	logger.Info("policies has been updated")
	return nil
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
	requestBatch := channel.NewWriter(ctx, m.requestBatch)
	for ctx.Err() == nil {
		reqBatch, err := stream.Recv()
		if err != nil {
			return connection.FilterStreamRPCError(err)
		}

		err = m.updatePolicies(reqBatch.Update)
		if err != nil {
			return err
		}

		logger.Debugf("new batch received at the mock sig verifier with %d requests.", len(reqBatch.Requests))
		requestBatch.Write(reqBatch)
		m.numBlocksReceived.Add(1)
	}

	return nil
}

func (m *SigVerifier) sendResponseBatch(
	ctx context.Context,
	stream protosigverifierservice.Verifier_StartStreamServer,
) error {
	requestBatch := channel.NewReader(ctx, m.requestBatch)
	for ctx.Err() == nil {
		reqBatch, ok := requestBatch.Read()
		if !ok {
			return errors.Wrap(ctx.Err(), "context ended")
		}
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

// GetUpdates returns the updates received.
func (m *SigVerifier) GetUpdates() []*protosigverifierservice.Update {
	return m.updates
}

// SetReturnErrorForUpdatePolicies configures the SigVerifier to return an error during policy updates.
// When setError is true, the verifier will signal an error during the update policies process.
// It is a one time event, after which the flag will return to false.
func (m *SigVerifier) SetReturnErrorForUpdatePolicies(setError bool) {
	m.returnErrForUpdatePolicies.Store(setError)
}

// GetPolicyUpdateCounter returns the number of policy updates to check progress.
func (m *SigVerifier) GetPolicyUpdateCounter() uint64 {
	return m.policyUpdateCounter.Load()
}

// ClearPolicies allows resetting the known policies to mimic a new instance.
func (m *SigVerifier) ClearPolicies() {
	m.updates = nil
}

// SendRequestBatchWithoutStream allows the caller to bypass the stream to send
// a request batch.
// Returns true if successful.
func (m *SigVerifier) SendRequestBatchWithoutStream(
	ctx context.Context,
	requestBatch *protosigverifierservice.RequestBatch,
) bool {
	return channel.NewWriter(ctx, m.requestBatch).Write(requestBatch)
}
