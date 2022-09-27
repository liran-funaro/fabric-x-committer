package shardsservice

import (
	"context"
	"io"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type shardsCoordinator struct {
	shards            *shardInstances
	phaseOneResponses chan []*PhaseOneResponse
	config            *ShardCoordinatorConfig
	logger            *logging.AppLogger
	UnimplementedShardsServer
}

func NewShardsCoordinator(conf *ShardCoordinatorConfig) *shardsCoordinator {
	logger := logging.New("shard coordinator")
	logger.Info("Initializing shards coordinator")

	phaseOneResponses := make(chan []*PhaseOneResponse, 10)

	si, err := newShardInstances(phaseOneResponses, conf.Database.RootDir)
	if err != nil {
		panic(err)
	}

	return &shardsCoordinator{
		shards:                    si,
		phaseOneResponses:         phaseOneResponses,
		config:                    conf,
		logger:                    logger,
		UnimplementedShardsServer: UnimplementedShardsServer{},
	}
}

func (s *shardsCoordinator) SetupShards(ctx context.Context, request *ShardsSetupRequest) (*Empty, error) {
	s.logger.Debugf("received SetupShards request with FirstShardId [%d] and LastShardId [%d]", request.FirstShardId, request.LastShardId)
	for shardID := request.FirstShardId; shardID <= request.LastShardId; shardID++ {
		if err := s.shards.setup(shardID); err != nil {
			return &Empty{}, err
		}
	}

	return &Empty{}, nil
}

func (s *shardsCoordinator) DeleteShards(ctx context.Context, _ *Empty) (*Empty, error) {
	s.logger.Debug("received DeleteShards request")
	err := s.shards.deleteAll()
	return &Empty{}, err
}

func (s *shardsCoordinator) StartPhaseOneStream(stream Shards_StartPhaseOneStreamServer) error {
	go s.retrievePhaseOneResponse(stream)

	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			return err
		}

		go func() {
			s.shards.executePhaseOne(requestBatch)
		}()
	}
}

func (s *shardsCoordinator) retrievePhaseOneResponse(stream Shards_StartPhaseOneStreamServer) error {
	go s.shards.accumulatedPhaseOneResponses(s.config.Limits.MaxPhaseOneResponseBatchItemCount, s.config.Limits.PhaseOneResponseCutTimeout)
	for {
		if err := stream.Send(
			&PhaseOneResponseBatch{
				Responses: <-s.phaseOneResponses,
			},
		); err != nil {
			return err
		}
	}
}

func (s *shardsCoordinator) StartPhaseTwoStream(stream Shards_StartPhaseTwoStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		go func() {
			s.shards.executePhaseTwo(requestBatch)
		}()
	}
}
