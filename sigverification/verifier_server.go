package sigverification

import (
	"context"
	"errors"
	"log"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

type verifierServer struct {
	verificationKey *Key
	executor        ParallelExecutor
}

func NewVerifierServer(config *ParallelExecutionConfig) *verifierServer {
	s := &verifierServer{}
	s.executor = NewParallelExecutor(s.verifyRequest, config)
	return s
}

func (s *verifierServer) SetVerificationKey(context context.Context, publicKey *Key) (*Empty, error) {
	if len(publicKey.SerializedBytes) == 0 {
		return nil, errors.New("invalid public key")
	}
	s.verificationKey = publicKey
	return &Empty{}, nil
}
func (s *verifierServer) StartStream(stream Verifier_StartStreamServer) error {
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
	if err := s.verifySignature(request.Tx); err != nil {
		response.ErrorMessage = err.Error()
	} else {
		response.IsValid = true
	}
	return response, nil
}

func (s *verifierServer) verifySignature(tx *token.Tx) error {
	// TODO: Impl
	return nil
}

func (s *verifierServer) mustEmbedUnimplementedVerifierServer() {}
