package shardsservice

import (
	"context"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type shardsCoordinator struct {
	shards            *shardInstances
	routineLimiter    *goroutineLimiter
	phaseOneResponses chan []*PhaseOneResponse
	config            *Configuration
	logger            *logging.AppLogger
	UnimplementedShardsServer
}

func NewShardsCoordinator(conf *Configuration) *shardsCoordinator {
	logger := logging.New("shard coordinator")
	logger.Info("Initializing shards coordinator")

	limiter := newGoroutineLimiter(conf.Limits.MaxGoroutines)
	phaseOneResponses := make(chan []*PhaseOneResponse, 10)

	// TODO: Need to read shards which already exists. For now, we assume a
	// fresh start of the shardservice. For failure and recovery, we need to
	// read existing shards

	si, err := newShardInstances(limiter, phaseOneResponses, conf.Database.RootDir)
	if err != nil {
		panic(err)
	}

	return &shardsCoordinator{
		shards:                    si,
		routineLimiter:            limiter,
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

		s.routineLimiter.add()
		go func() {
			defer s.routineLimiter.done()
			s.shards.executePhaseOne(requestBatch)
		}()
	}
}

func (s *shardsCoordinator) retrievePhaseOneResponse(stream Shards_StartPhaseOneStreamServer) error {
	go s.shards.accumulatedPhaseOneResponses(s.config.Limits.MaxPhaseOneResponseBatchItemCount, s.config.Limits.PhaseOneResponseCutTimeout)
	for {
		// TODO: we need to send only one response per transaction, i.e., wither commit or cannot commit
		// irrespective of the number of phaseOneRequests for a given transaction
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
			return err
		}

		s.routineLimiter.add()
		go func() {
			defer s.routineLimiter.done()
			s.shards.executePhaseTwo(requestBatch)
		}()
	}
}
