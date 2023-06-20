package testutil

import (
	"context"
	"io"

	"github.ibm.com/distributed-trust-research/scalable-committer/protos/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
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

type ShardsGrpcServer struct {
	grpcServer       *grpc.Server
	ShardsServerImpl *ShardsServerImpl
}

func StartsShardsGrpcServers(
	phaseOneBehavior func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch,
	endpoints []*connection.Endpoint,
) ([]*ShardsGrpcServer, error) {

	servers := make([]*ShardsGrpcServer, len(endpoints))
	for i, endpoint := range endpoints {
		s, err := NewShardsGrpcServer(phaseOneBehavior, endpoint)
		if err != nil {
			return nil, err
		}
		servers[i] = s
	}
	return servers, nil
}

func NewShardsGrpcServer(
	phaseOneBehavior func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch,
	endpoint *connection.Endpoint,
) (*ShardsGrpcServer, error) {
	grpcServer := grpc.NewServer()
	shardsServerImpl := &ShardsServerImpl{
		PhaseOneBehavior: phaseOneBehavior,
		Stats:            &ShardsServerStats{},
	}

	shardsservice.RegisterShardsServer(grpcServer, shardsServerImpl)
	if err := startGrpcServer(endpoint, grpcServer); err != nil {
		return nil, err
	}

	return &ShardsGrpcServer{
		grpcServer:       grpcServer,
		ShardsServerImpl: shardsServerImpl,
	}, nil

}

func (s *ShardsGrpcServer) Stop() {
	s.grpcServer.GracefulStop()
}

type ShardsServerStats struct {
	PhaseOneBatchesServed int
	PhaseTwoBatchesServed int
}

type ShardsServerImpl struct {
	shardsservice.UnimplementedShardsServer
	PhaseOneBehavior func(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch
	Stats            *ShardsServerStats
}

func (s *ShardsServerImpl) DeleteShards(context.Context, *shardsservice.Empty) (*shardsservice.Empty, error) {
	return &shardsservice.Empty{}, nil
}

func (s *ShardsServerImpl) SetupShards(context.Context, *shardsservice.ShardsSetupRequest) (*shardsservice.Empty, error) {
	return &shardsservice.Empty{}, nil
}

func (s *ShardsServerImpl) StartPhaseOneStream(stream shardsservice.Shards_StartPhaseOneStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		responseBatch := s.PhaseOneBehavior(requestBatch)
		if err := stream.Send(responseBatch); err != nil {
			return err
		}
		s.Stats.PhaseOneBatchesServed++
	}
}

func (s *ShardsServerImpl) StartPhaseTwoStream(stream shardsservice.Shards_StartPhaseTwoStreamServer) error {
	for {
		_, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		s.Stats.PhaseTwoBatchesServed++
	}
}
