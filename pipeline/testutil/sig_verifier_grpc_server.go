package testutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
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
	grpcServer *grpc.Server
}

func NewSigVerifierGrpcServer(
	behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch,
	port int,
) (*SigVerifierGrpcServer, error) {

	grpcServer := grpc.NewServer()
	sigverification.RegisterVerifierServer(grpcServer,
		&sigVerifierImpl{
			behavior: behavior,
		},
	)

	if err := startGrpcServer(port, grpcServer); err != nil {
		return nil, err
	}

	return &SigVerifierGrpcServer{
		grpcServer: grpcServer,
	}, nil
}

func (s *SigVerifierGrpcServer) Stop() {
	s.grpcServer.GracefulStop()
}

type sigVerifierImpl struct {
	sigverification.UnimplementedVerifierServer
	behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch
}

func (s *sigVerifierImpl) SetVerificationKey(context context.Context, key *sigverification.Key) (*sigverification.Empty, error) {
	return nil, nil
}

func (s *sigVerifierImpl) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		responseBatch := s.behavior(requestBatch)
		if err := stream.Send(responseBatch); err != nil {
			return err
		}
	}
}

func startGrpcServer(port int, grpcServer *grpc.Server) error {
	//bufconn.Listen(1024 * 1024)
	address := fmt.Sprintf("localhost:%d", port)
	go func() {
		lis, err := net.Listen("tcp", address)
		if err != nil {
			panic(fmt.Sprintf("Error while starting test grpc server: %s", err))
		}

		err = grpcServer.Serve(lis)
		if err != nil {
			panic(fmt.Sprintf("Error while starting test grpc server: %s", err))
		}
	}()

	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", address, 1*time.Second)
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
