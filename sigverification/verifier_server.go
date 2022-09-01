package sigverification

import (
	"context"
	"errors"
)

type verifierServer struct {
	verificationKey *Key
	executor        ParallelExecutor
	verifier        TxVerifier
}

func NewVerifierServer(config *ParallelExecutionConfig, verificationScheme Scheme) *verifierServer {
	s := &verifierServer{}
	s.executor = NewParallelExecutor(s.verifyRequest, config)
	s.verifier = NewTxVerifier(verificationScheme)
	return s
}

func (s *verifierServer) SetVerificationKey(context context.Context, verificationKey *Key) (*Empty, error) {
	if !s.verifier.IsVerificationKeyValid(verificationKey) {
		logger.Info("Attempted to set an empty verification key.")
		return nil, errors.New("invalid public key")
	}
	s.verificationKey = verificationKey
	logger.Info("Set a new verification key.")
	return &Empty{}, nil
}
func (s *verifierServer) StartStream(stream Verifier_StartStreamServer) error {
	if !s.verifier.IsVerificationKeyValid(s.verificationKey) {
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

func (s *verifierServer) handleOutputs(stream Verifier_StartStreamServer) {
	for {
		outputs := <-s.executor.Outputs()
		logger.Debugf("Received %d responses from parallel executor: %v", len(outputs), outputs)
		err := stream.Send(&ResponseBatch{Responses: outputs})
		if err != nil {
			logger.Infof("Failed to send %d responses to client.", len(outputs))
			//TODO: Replace panics with error handling
			panic(err)
		}
		logger.Debugf("Forwarded %d responses to client.", len(outputs))
	}
}

func (s *verifierServer) verifyRequest(request *Request) (*Response, error) {
	response := &Response{
		BlockNum: request.GetBlockNum(),
		TxNum:    request.GetTxNum(),
	}
	if err := s.verifier.VerifyTx(s.verificationKey, request.Tx); err != nil {
		response.ErrorMessage = err.Error()
	} else {
		response.IsValid = true
	}
	return response, nil
}

func (s *verifierServer) mustEmbedUnimplementedVerifierServer() {}
