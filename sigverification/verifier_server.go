package sigverification

import (
	"context"
	"errors"
	"log"
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
		return nil, errors.New("invalid public key")
	}
	s.verificationKey = verificationKey
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
			log.Printf("failed to serve request: %v", err)
			return err
		}

		s.executor.Submit(requestBatch.Requests)
	}
}

func (s *verifierServer) handleOutputs(stream Verifier_StartStreamServer) {
	for {
		outputs := <-s.executor.Outputs()
		err := stream.Send(&ResponseBatch{Responses: outputs})
		if err != nil {
			//TODO: Replace panics with error handling
			panic(err)
		}
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
