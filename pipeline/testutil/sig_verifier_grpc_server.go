package testutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

var DefaultSigVerifierBehavior = func(requestBatch *sigverification.RequestBatch) *sigverification.ResponseBatch {
	responseBatch := &sigverification.ResponseBatch{}
	for _, request := range requestBatch.Requests {
		responseBatch.Responses = append(responseBatch.Responses, &sigverification.Response{
			BlockNum: request.BlockNum,
			TxNum:    request.TxNum,
			IsValid:  true,
		})
	}
	return responseBatch
}

type SigVerifierGrpcServer struct {
	grpcServer      *grpc.Server
	SigVerifierImpl *SigVerifierImpl
}

func StartsSigVerifierGrpcServers(
	behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch,
	addresses []*connection.Endpoint,
) ([]*SigVerifierGrpcServer, error) {

	servers := make([]*SigVerifierGrpcServer, len(addresses))
	for i, a := range addresses {
		s, err := NewSigVerifierGrpcServer(behavior, a)
		if err != nil {
			return nil, err
		}
		servers[i] = s
	}
	return servers, nil
}

func NewSigVerifierGrpcServer(
	behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch,
	address *connection.Endpoint,
) (*SigVerifierGrpcServer, error) {

	grpcServer := grpc.NewServer()
	sigVerifierImpl := &SigVerifierImpl{
		Behavior: behavior,
		Stats:    &SigVerifierStats{},
	}

	sigverification.RegisterVerifierServer(grpcServer, sigVerifierImpl)
	if err := startGrpcServer(address, grpcServer); err != nil {
		return nil, err
	}

	return &SigVerifierGrpcServer{
		grpcServer:      grpcServer,
		SigVerifierImpl: sigVerifierImpl,
	}, nil
}

func (s *SigVerifierGrpcServer) Stop() {
	s.grpcServer.GracefulStop()
}

type SigVerifierStats struct {
	NumBatchesServed int
}

type SigVerifierImpl struct {
	sigverification.UnimplementedVerifierServer
	Behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch
	Stats    *SigVerifierStats
}

func (s *SigVerifierImpl) SetVerificationKey(context context.Context, key *sigverification.Key) (*sigverification.Empty, error) {
	return &sigverification.Empty{}, nil
}

func (s *SigVerifierImpl) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		responseBatch := s.Behavior(requestBatch)
		if err := stream.Send(responseBatch); err != nil {
			return err
		}
		s.Stats.NumBatchesServed++
	}
}

func startGrpcServer(address *connection.Endpoint, grpcServer *grpc.Server) error {
	//bufconn.Listen(1024 * 1024)
	go func() {
		lis, err := net.Listen("tcp", address.Address())
		if err != nil {
			panic(fmt.Sprintf("Error while starting test grpc server: %s", err))
		}

		err = grpcServer.Serve(lis)
		if err != nil {
			panic(fmt.Sprintf("Error while starting test grpc server: %s", err))
		}
	}()

	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", address.Address(), 1*time.Second)
		if err != nil {
			if i == 9 {
				return err
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		return conn.Close()
	}
	return nil
}
