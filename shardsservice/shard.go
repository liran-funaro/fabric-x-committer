package shardsservice

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/shardsservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/workerpool"
)

type shard struct {
	id       uint32
	path     string
	database db.Database
	mu       sync.RWMutex

	pendingCommits    pendingcommits.PendingCommits
	phaseOneResponses *phaseOneResponse
	phaseOnePool      *workerpool.WorkerPool
	wg                sync.WaitGroup
	logger            *logging.Logger
	metrics           *metrics.Metrics
}

func newShard(id uint32, dbConfig *DatabaseConfig, limits *LimitsConfig, metrics *metrics.Metrics) (*shard, error) {
	database, err := db.OpenDb(dbConfig.Type, shardFilePath(dbConfig.RootDir, id))
	if err != nil {
		return nil, err
	}

	const channelCapacity = 10

	return &shard{
		id:             id,
		path:           shardFilePath(dbConfig.RootDir, id),
		database:       database,
		mu:             sync.RWMutex{},
		pendingCommits: pendingcommits.NewCondPendingCommits(limits.MaxPendingCommitsBufferSize),
		phaseOnePool: workerpool.New(&workerpool.Config{
			Parallelism:     int(limits.MaxPhaseOneProcessingWorkers),
			ChannelCapacity: channelCapacity,
		}),
		phaseOneResponses: &phaseOneResponse{},
		wg:                sync.WaitGroup{},
		logger:            logging.New("shard"),
		metrics:           metrics,
	}, nil
}

func (s *shard) executePhaseOne(requests txIDToSerialNumbers) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.metrics.Enabled {
		s.metrics.IncomingTxs.With(metrics.ShardId(s.id)).Add(float64(len(requests)))
	}

	for tID, serialNumbers := range requests {
		if s.metrics.Enabled {
			s.metrics.RequestTracer.AddEvent(tID, fmt.Sprintf("Started phase one on shard %d", s.id))
		}
		s.wg.Add(1)
		s.phaseOnePool.Run(func(tID pendingcommits.TxID, serialNumbers *shardsservice.SerialNumbers) func() {
			return func() {
				s.logger.Debugf("shardID [%d] validating TxID [%v]", s.id, tID)
				defer s.wg.Done()

				resp := &shardsservice.PhaseOneResponse{
					BlockNum: tID.BlkNum,
					TxNum:    tID.TxNum,
				}

				startWait := time.Now()
				s.pendingCommits.WaitTillNotExist(serialNumbers.GetSerialNumbers())

				startRead := time.Now()
				doNoExist, _ := s.database.DoNotExist(serialNumbers.GetSerialNumbers())
				if s.metrics.Enabled {
					s.metrics.RequestTracer.AddEventAt(tID, fmt.Sprintf("Start wait on shard %d", s.id), startWait)
					s.metrics.RequestTracer.AddEventAt(tID, fmt.Sprintf("Start read on shard %d", s.id), startRead)
					s.metrics.SNReadDuration.With(metrics.ShardId(s.id)).Observe(float64(time.Now().Sub(startRead)))
					s.metrics.SNReadSize.With(metrics.ShardId(s.id)).Observe(float64(len(serialNumbers.GetSerialNumbers())))
				}
				for _, notExist := range doNoExist {
					if !notExist {
						s.logger.Debugf("shardID [%d] invalidates TxID [%v]", s.id, tID)
						resp.Status = shardsservice.PhaseOneResponse_CANNOT_COMMITTED
						s.phaseOneResponses.add(resp)
						return
					}
				}

				resp.Status = shardsservice.PhaseOneResponse_CAN_COMMIT
				s.pendingCommits.Add(tID, serialNumbers.SerialNumbers)
				s.phaseOneResponses.add(resp)
				if s.metrics.Enabled {
					s.metrics.RequestTracer.AddEvent(tID, fmt.Sprintf("Finished read on shard %d", s.id))
					s.metrics.PendingCommitsSNs.Set(s.pendingCommits.CountSNs())
					s.metrics.PendingCommitsTxIds.Set(s.pendingCommits.CountTxs())
				}
				s.logger.Debugf("shardID [%d] successfully validated TxID [%v]", s.id, tID)
			}
		}(tID, serialNumbers))
	}
}

func (s *shard) executePhaseTwo(requests txIDToInstruction) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	validSNs := make([]token.SerialNumber, 0)

	txIds := make([]pendingcommits.TxID, 0, len(requests))
	for txID, instruction := range requests {
		// TODO: commit in progress should be recorded for recovery
		serialNumbers := s.pendingCommits.Get(txID)
		if instruction == shardsservice.PhaseTwoRequest_COMMIT {
			validSNs = append(validSNs, serialNumbers...)
		}
		txIds = append(txIds, txID)
	}

	s.logger.Debugf("shard [%d] committing [%d] serial numbers", s.id, len(validSNs))
	startCommit := time.Now()
	if err := s.database.Commit(validSNs); err != nil {
		panic(err)
	}
	s.pendingCommits.DeleteBatch(txIds)

	if s.metrics.Enabled {
		s.metrics.SNCommitDuration.With(metrics.ShardId(s.id)).Observe(float64(time.Now().Sub(startCommit)))
		s.metrics.SNCommitSize.With(metrics.ShardId(s.id)).Observe(float64(len(validSNs)))
		s.metrics.CommittedSNs.With(metrics.ShardId(s.id)).Add(float64(len(validSNs)))
		s.metrics.PendingCommitsSNs.Set(s.pendingCommits.CountSNs())
		s.metrics.PendingCommitsTxIds.Set(s.pendingCommits.CountTxs())
	}
}

func (s *shard) accumulatedPhaseOneResponse() []*shardsservice.PhaseOneResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.phaseOneResponses.getAndRemoveAll()
}

func (s *shard) delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wg.Wait()

	s.database.Close()
	s.phaseOneResponses = nil
	return os.RemoveAll(s.path)
}

type phaseOneResponse struct {
	responses []*shardsservice.PhaseOneResponse
	mu        sync.RWMutex
}

func (r *phaseOneResponse) add(resp *shardsservice.PhaseOneResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.responses = append(r.responses, resp)
}

func (r *phaseOneResponse) getAndRemoveAll() []*shardsservice.PhaseOneResponse {
	r.mu.Lock()
	defer r.mu.Unlock()

	responses := r.responses
	r.responses = []*shardsservice.PhaseOneResponse{}
	return responses
}
