package verifierserver

import (
	"context"
	"errors"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("verifierserver")

type verifierServer struct {
	sigverification.UnimplementedVerifierServer
	verificationKey *sigverification.Key
	executor        parallelexecutor.ParallelExecutor
	verifier        signature.TxVerifier
}

func New(config *parallelexecutor.Config, verificationScheme signature.Scheme) *verifierServer {
	s := &verifierServer{}
	s.executor = parallelexecutor.New(s.verifyRequest, config)
	s.verifier = signature.NewTxVerifier(verificationScheme)
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

	go s.handleOutputs(stream)

	for {
		//TODO: Add cancel
		requestBatch, err := stream.Recv()
		if err != nil {
			logger.Infof("failed to serve request: %v", err)
			return err
		}
		logger.Debugf("Received %d requests from client: %v", len(requestBatch.Requests), requestBatch)

		s.executor.Submit(requestBatch.Requests)
	}
}

func (s *verifierServer) handleOutputs(stream sigverification.Verifier_StartStreamServer) {
	for {
		outputs := <-s.executor.Outputs()
		logger.Debugf("Received %d responses from parallel executor: %v", len(outputs), outputs)
		err := stream.Send(&sigverification.ResponseBatch{Responses: outputs})
		if err != nil {
			logger.Infof("Failed to send %d responses to client.", len(outputs))
			//TODO: Replace panics with error handling
			panic(err)
		}
		logger.Debugf("Forwarded %d responses to client.", len(outputs))
	}
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
