package testutil

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
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
	t          *testing.T
	grpcServer *grpc.Server
}

func NewShardsServer(
	t *testing.T,
	phaseOneBehavior func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch,
	port int,
) *ShardsServer {
	grpcServer := grpc.NewServer()
	shardsservice.RegisterServerServer(grpcServer,
		&shardsServerImpl{
			t:                t,
			phaseOneBehavior: phaseOneBehavior,
		},
	)

	startGrpcServer(t, port, grpcServer)

	return &ShardsServer{
		t:          t,
		grpcServer: grpcServer,
	}

}

func (s *ShardsServer) Stop() {
	s.grpcServer.GracefulStop()
}

type shardsServerImpl struct {
	shardsservice.UnimplementedServerServer
	t                *testing.T
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
			require.NoError(s.t, err)
		}
		responseBatch := s.phaseOneBehavior(requestBatch)
		require.NoError(s.t, stream.Send(responseBatch))
	}
}

func (s *shardsServerImpl) StartPhaseTwoStream(stream shardsservice.Server_StartPhaseTwoStreamServer) error {
	for {
		_, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			require.NoError(s.t, err)
		}
	}
}
