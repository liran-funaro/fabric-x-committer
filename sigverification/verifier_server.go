package sigverification

import (
	"context"
	"errors"
	"log"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

type verifierServer struct {
	verificationKey *Key
}

func NewVerifierServer() *verifierServer {
	return &verifierServer{}
}

func (s *verifierServer) SetVerificationKey(context context.Context, publicKey *Key) (*Empty, error) {
	if len(publicKey.SerializedBytes) == 0 {
		return nil, errors.New("invalid public key")
	}
	s.verificationKey = publicKey
	return &Empty{}, nil
}
func (s *verifierServer) StartStream(stream Verifier_StartStreamServer) error {
	for {
		//TODO: Add cancel
		requestBatch, err := stream.Recv()
		if err != nil {
			log.Printf("failed to serve request: %v", err)
			return err
		}

		responses := s.verifyRequests(requestBatch.Requests)
		err = stream.Send(&ResponseBatch{Responses: responses})
		if err != nil {
			//TODO: Replace panics with error handling
			//TODO: Add logging
			panic(err)
		}
	}
}

func (s *verifierServer) verifyRequests(requests []*Request) []*Response {
	responses := make([]*Response, len(requests))
	//TODO: Parallelize
	for i, request := range requests {
		responses[i] = s.verifyRequest(request)
	}
	return responses
}

func (s *verifierServer) verifyRequest(request *Request) *Response {
	response := &Response{
		BlockNum: request.GetBlockNum(),
		TxNum:    request.GetTxNum(),
	}
	if err := s.verifySignature(request.Tx); err != nil {
		response.ErrorMessage = err.Error()
	} else {
		response.IsValid = true
	}
	return response
}

func (s *verifierServer) verifySignature(tx *token.Tx) error {
	// TODO: Impl
	return nil
}

func (s *verifierServer) mustEmbedUnimplementedVerifierServer() {}
