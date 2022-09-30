package verifierserver

import (
	"context"
	"errors"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/streamhandler"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("verifierserver")

type verifierServer struct {
	sigverification.UnimplementedVerifierServer
	verificationScheme signature.Scheme
	streamHandler      *streamhandler.StreamHandler
	verifier           signature.TxVerifier
	metricsEnabled     bool
}

func New(parallelExecutionConfig *parallelexecutor.Config, verificationScheme signature.Scheme, metricsEnabled bool) *verifierServer {
	s := &verifierServer{verificationScheme: verificationScheme, metricsEnabled: metricsEnabled}

	executor := parallelexecutor.New(s.verifyRequest, parallelExecutionConfig, metricsEnabled)
	s.streamHandler = streamhandler.New(
		func(batch *sigverification.RequestBatch) {
			if s.metricsEnabled {
				metrics.TxsReceived.Add(float64(len(batch.Requests)))
				metrics.BatchesReceived.Inc()
			}
			executor.Submit(batch.Requests)
		},
		func() streamhandler.Output {
			outputs := <-executor.Outputs()
			if s.metricsEnabled {
				metrics.TxsSent.Add(float64(len(outputs)))
				metrics.BatchesSent.Inc()
			}
			return &sigverification.ResponseBatch{Responses: outputs}
		})
	return s
}

func (s *verifierServer) SetVerificationKey(context context.Context, verificationKey *sigverification.Key) (*sigverification.Empty, error) {
	if verificationKey == nil {
		logger.Info("Attempted to set an empty verification key.")
		return nil, errors.New("invalid public key")
	}
	verifier, err := signature.NewTxVerifier(s.verificationScheme, verificationKey.GetSerializedBytes())
	if err != nil {
		return nil, err
	}
	s.verifier = verifier

	logger.Info("Set a new verification key.")
	return &sigverification.Empty{}, nil
}
func (s *verifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	//if s.verifier == nil {
	//	return errors.New("no verification key set")
	//}
	if s.metricsEnabled {
		metrics.ActiveStreams.Inc()
		defer metrics.ActiveStreams.Dec()
	}
	s.streamHandler.HandleStream(stream)
	logger.Debug("Interrupted stream.")
	return nil
}

func (s *verifierServer) verifyRequest(request *sigverification.Request) (*sigverification.Response, error) {
	response := &sigverification.Response{
		BlockNum: request.GetBlockNum(),
		TxNum:    request.GetTxNum(),
	}
	if err := s.verifier.VerifyTx(request.Tx); err != nil {
		response.ErrorMessage = err.Error()
	} else {
		response.IsValid = true
	}
	return response, nil
}
