package shardsservice

import (
	"context"
	"io"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
	"go.opentelemetry.io/otel/attribute"
)

var logger = logging.New("shard coordinator")

type shardsCoordinator struct {
	UnimplementedShardsServer
	shards            *shardInstances
	phaseOneResponses chan []*PhaseOneResponse
	limits            *LimitsConfig
	phaseOnePool      *workerpool.WorkerPool
	phaseTwoPool      *workerpool.WorkerPool
	metrics           *metrics.Metrics
}

func NewShardsCoordinator(database *DatabaseConfig, limits *LimitsConfig, metrics *metrics.Metrics) *shardsCoordinator {

	const channelCapacity = 10
	logger.Infof("Initializing shards coordinator:\n"+
		"\tDatabase: %s\n"+
		"\tLimits:\n"+
		"\t\tMax buffer sizes: %d (shard instances), %d (pending commits)\n"+
		"\t\tWorkers: %d (phase 1), %d (phase 2), Channel capacity: %d\n"+
		"\t\tCut off: %d %v\n"+
		"\tTotal metrics: %d", database.Type, limits.MaxShardInstancesBufferSize, limits.MaxPendingCommitsBufferSize, limits.MaxPhaseOneProcessingWorkers, limits.MaxPhaseTwoProcessingWorkers, channelCapacity, limits.MaxPhaseOneResponseBatchItemCount, limits.PhaseOneResponseCutTimeout, len(metrics.AllMetrics()))

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
		metrics: metrics,
	}
}

func (s *shardsCoordinator) SetupShards(ctx context.Context, request *ShardsSetupRequest) (*Empty, error) {
	logger.Info("received SetupShards request with FirstShardId [%d] and LastShardId [%d]", request.FirstShardId, request.LastShardId)
	for shardID := request.FirstShardId; shardID <= request.LastShardId; shardID++ {
		if err := s.shards.setup(shardID, s.limits); err != nil {
			return &Empty{}, err
		}
	}

	return &Empty{}, nil
}

func (s *shardsCoordinator) DeleteShards(ctx context.Context, _ *Empty) (*Empty, error) {
	logger.Debug("received DeleteShards request")
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
		logger.Debugf("Received batch of %d TXs for P1.", len(requestBatch.Requests))

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
		responses := <-s.phaseOneResponses
		logger.Debugf("Returning batch of %d TXs from P1.", len(responses))
		end := time.Now()
		if err := stream.Send(
			&PhaseOneResponseBatch{
				Responses: responses,
			},
		); err != nil {
			return err
		}
		if s.metrics.Enabled {
			s.metrics.ShardsPhaseOneResponseChLength.Set(len(s.phaseOneResponses))
			for _, response := range responses {
				txID := pendingcommits.TxID{TxNum: response.TxNum, BlkNum: response.BlockNum}
				s.metrics.RequestTracer.AddEventAt(txID, "Finished calculation on all shards.", end)
				s.metrics.RequestTracer.End(txID, attribute.String(metrics.StatusLabel, response.Status.String()))
			}
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
