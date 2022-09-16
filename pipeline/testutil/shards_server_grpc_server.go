package testutil

import (
	"context"
	"io"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"google.golang.org/grpc"
)

var DefaultPhaseOneBehavior = func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch {
	responseBatch := &shardsservice.PhaseOneResponseBatch{}
	for _, request := range requestBatch.Requests {
		responseBatch.Responses = append(responseBatch.Responses, &shardsservice.PhaseOneResponse{
			BlockNum: request.BlockNum,
			TxNum:    request.TxNum,
			Status:   shardsservice.PhaseOneResponse_COMMIT_DONE,
		})
	}
	return responseBatch
}

type ShardsServer struct {
	grpcServer *grpc.Server
}

func NewShardsServer(
	phaseOneBehavior func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch,
	port int,
) (*ShardsServer, error) {
	grpcServer := grpc.NewServer()
	shardsservice.RegisterServerServer(grpcServer,
		&shardsServerImpl{
			phaseOneBehavior: phaseOneBehavior,
		},
	)

	if err := startGrpcServer(port, grpcServer); err != nil {
		return nil, err
	}

	return &ShardsServer{
		grpcServer: grpcServer,
	}, nil

}

func (s *ShardsServer) Stop() {
	s.grpcServer.GracefulStop()
}

type shardsServerImpl struct {
	shardsservice.UnimplementedServerServer
	phaseOneBehavior func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch
}

func (s *shardsServerImpl) DeleteShards(context.Context, *shardsservice.Empty) (*shardsservice.Empty, error) {
	return &shardsservice.Empty{}, nil
}

func (s *shardsServerImpl) SetupShards(context.Context, *shardsservice.ShardsSetupRequest) (*shardsservice.Empty, error) {
	return &shardsservice.Empty{}, nil
}

func (s *shardsServerImpl) StartPhaseOneStream(stream shardsservice.Server_StartPhaseOneStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		responseBatch := s.phaseOneBehavior(requestBatch)
		if err := stream.Send(responseBatch); err != nil {
			return err
		}
	}
}

func (s *shardsServerImpl) StartPhaseTwoStream(stream shardsservice.Server_StartPhaseTwoStreamServer) error {
	for {
		_, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}
