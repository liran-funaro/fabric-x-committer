package sigverifiermock

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("sigverifier_mock")

// MockSigVerifier is a mock implementation of the protosignverifierservice.VerifierServer.
// MockSigVerifier marks valid and invalid flag as follows:
// - when the tx has empty signature, it is invalid.
// - when the tx has non-empty signature, it is valid.
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
	logger.Info("Verification key has been set")
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
			respBatch.Responses[i] = &protosigverifierservice.Response{
				BlockNum: req.BlockNum,
				TxNum:    req.TxNum,
				IsValid:  len(req.GetTx().GetSignature()) > 0,
			}
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

// StartMockSVService starts a specified number of mock verifier service.
func StartMockSVService(
	numService int,
) ([]*connection.ServerConfig, []*MockSigVerifier, []*grpc.Server) {
	sc := make([]*connection.ServerConfig, 0, numService)
	for i := 0; i < numService; i++ {
		sc = append(sc, &connection.ServerConfig{
			Endpoint: connection.Endpoint{
				Host: "localhost",
				Port: 0,
			},
		})
	}

	grpcSrvs := make([]*grpc.Server, numService)
	svs := make([]*MockSigVerifier, numService)
	for i, s := range sc {
		svs[i] = NewMockSigVerifier()

		index := i
		config := s

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			connection.RunServerMain(config, func(grpcServer *grpc.Server, actualListeningPort int) {
				grpcSrvs[index] = grpcServer
				config.Endpoint.Port = actualListeningPort
				protosigverifierservice.RegisterVerifierServer(grpcServer, svs[index])
				wg.Done()
			})
		}()
		wg.Wait()
	}

	return sc, svs, grpcSrvs
}
