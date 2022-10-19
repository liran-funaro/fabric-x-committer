package shardsservice

import (
	"context"
	"io"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
)

type shardsCoordinator struct {
	UnimplementedShardsServer
	shards            *shardInstances
	phaseOneResponses chan []*PhaseOneResponse
	limits            *LimitsConfig
	phaseOnePool      *workerpool.WorkerPool
	phaseTwoPool      *workerpool.WorkerPool
	logger            *logging.Logger
	metrics           *metrics.Metrics
}

func NewShardsCoordinator(database *DatabaseConfig, limits *LimitsConfig, metrics *metrics.Metrics) *shardsCoordinator {
	logger := logging.New("shard coordinator")
	logger.Info("Initializing shards coordinator")

	const channelCapacity = 10
	phaseOneResponses := make(chan []*PhaseOneResponse, channelCapacity)
	if metrics.Enabled {
		metrics.ShardsPhaseOneResponseChLength.SetCapacity(channelCapacity)
	}

	si, err := newShardInstances(phaseOneResponses, database, limits, metrics)
	if err != nil {
		panic(err)
	}

	return &shardsCoordinator{
		shards:            si,
		phaseOneResponses: phaseOneResponses,
		limits:            limits,
		phaseOnePool: workerpool.New(&workerpool.Config{
			Parallelism:     int(limits.MaxPhaseOneProcessingWorkers),
			ChannelCapacity: channelCapacity,
		}),
		phaseTwoPool: workerpool.New(&workerpool.Config{
			Parallelism:     int(limits.MaxPhaseTwoProcessingWorkers),
			ChannelCapacity: channelCapacity,
		}),
		logger:  logger,
		metrics: metrics,
	}
}

func (s *shardsCoordinator) SetupShards(ctx context.Context, request *ShardsSetupRequest) (*Empty, error) {
	s.logger.Debugf("received SetupShards request with FirstShardId [%d] and LastShardId [%d]", request.FirstShardId, request.LastShardId)
	for shardID := request.FirstShardId; shardID <= request.LastShardId; shardID++ {
		if err := s.shards.setup(shardID, s.limits); err != nil {
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

		s.phaseOnePool.Run(func(requestBatch *PhaseOneRequestBatch) func() {
			return func() {
				s.shards.executePhaseOne(requestBatch)
			}
		}(requestBatch))
	}
}

func (s *shardsCoordinator) retrievePhaseOneResponse(stream Shards_StartPhaseOneStreamServer) error {
	go s.shards.accumulatedPhaseOneResponses(s.limits.MaxPhaseOneResponseBatchItemCount, s.limits.PhaseOneResponseCutTimeout)
	for {
		if err := stream.Send(
			&PhaseOneResponseBatch{
				Responses: <-s.phaseOneResponses,
			},
		); err != nil {
			return err
		}
		if s.metrics.Enabled {
			s.metrics.ShardsPhaseOneResponseChLength.Set(len(s.phaseOneResponses))
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

		s.phaseTwoPool.Run(func(requestBatch *PhaseTwoRequestBatch) func() {
			return func() {
				s.shards.executePhaseTwo(requestBatch)
			}
		}(requestBatch))
	}
}
