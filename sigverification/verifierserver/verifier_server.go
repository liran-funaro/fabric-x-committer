package verifierserver

import (
	"context"
	"errors"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/streamhandler"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("verifierserver")

type verifierServer struct {
	sigverification.UnimplementedVerifierServer
	verificationKey *sigverification.Key
	streamHandler   *streamhandler.StreamHandler
	verifier        signature.TxVerifier
}

func New(config *parallelexecutor.Config, verificationScheme signature.Scheme) *verifierServer {
	s := &verifierServer{}
	s.verifier = signature.NewTxVerifier(verificationScheme)

	executor := parallelexecutor.New(s.verifyRequest, config)
	s.streamHandler = streamhandler.New(
		func(batch *sigverification.RequestBatch) {
			executor.Submit(batch.Requests)
		},
		func() streamhandler.Output {
			return &sigverification.ResponseBatch{Responses: <-executor.Outputs()}
		})
	return s
}

func (s *verifierServer) SetVerificationKey(context context.Context, verificationKey *sigverification.Key) (*sigverification.Empty, error) {
	if verificationKey == nil || !s.verifier.IsVerificationKeyValid(verificationKey.SerializedBytes) {
		logger.Info("Attempted to set an empty verification key.")
		return nil, errors.New("invalid public key")
	}
	s.verificationKey = verificationKey
	logger.Info("Set a new verification key.")
	return &sigverification.Empty{}, nil
}
func (s *verifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	if s.verificationKey == nil || !s.verifier.IsVerificationKeyValid(s.verificationKey.SerializedBytes) {
		return errors.New("no verification key set")
	}
	s.streamHandler.HandleStream(stream)
	return nil
}

func (s *verifierServer) verifyRequest(request *sigverification.Request) (*sigverification.Response, error) {
	response := &sigverification.Response{
		BlockNum: request.GetBlockNum(),
		TxNum:    request.GetTxNum(),
	}
	if err := s.verifier.VerifyTx(s.verificationKey.SerializedBytes, request.Tx); err != nil {
		response.ErrorMessage = err.Error()
	} else {
		response.IsValid = true
	}
	return response, nil
}
