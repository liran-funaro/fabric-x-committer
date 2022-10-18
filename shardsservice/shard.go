package shardsservice

import (
	"os"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/goleveldb"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/pendingcommits"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/workerpool"
)

type shard struct {
	id   uint32
	path string
	db   database
	mu   sync.RWMutex

	pendingCommits    pendingcommits.PendingCommits
	phaseOneResponses *phaseOneResponse
	phaseOnePool      *workerpool.WorkerPool
	wg                sync.WaitGroup
	logger            *logging.Logger
	metrics           *metrics.Metrics
}

type database interface {
	DoNotExist(keys [][]byte) ([]bool, error)
	Commit(keys [][]byte) error
	Close()
}

func newShard(id uint32, path string, limits *LimitsConfig, metrics *metrics.Metrics) (*shard, error) {
	//db, err := rocksdb.Open(path)
	db, err := goleveldb.Open(path)
	if err != nil {
		return nil, err
	}

	const channelCapacity = 10

	return &shard{
		id:             id,
		path:           path,
		db:             db,
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
		s.wg.Add(1)
		s.phaseOnePool.Run(func(tID pendingcommits.TxID, serialNumbers *SerialNumbers) func() {
			return func() {
				s.logger.Debugf("shardID [%d] validating TxID [%v]", s.id, tID)
				defer s.wg.Done()

				resp := &PhaseOneResponse{
					BlockNum: tID.BlkNum,
					TxNum:    tID.TxNum,
				}

				s.pendingCommits.WaitTillNotExist(serialNumbers.GetSerialNumbers())

				startRead := time.Now()
				doNoExist, _ := s.db.DoNotExist(serialNumbers.GetSerialNumbers())
				if s.metrics.Enabled {
					s.metrics.SNReadDuration.With(metrics.ShardId(s.id)).Set(float64(time.Now().Sub(startRead)))
					s.metrics.SNReadSize.With(metrics.ShardId(s.id)).Set(float64(len(serialNumbers.GetSerialNumbers())))
				}
				for _, notExist := range doNoExist {
					if !notExist {
						s.logger.Debugf("shardID [%d] invalidates TxID [%v]", s.id, tID)
						resp.Status = PhaseOneResponse_CANNOT_COMMITTED
						s.phaseOneResponses.add(resp)
						return
					}
				}

				resp.Status = PhaseOneResponse_CAN_COMMIT
				s.pendingCommits.Add(tID, serialNumbers.SerialNumbers)
				s.phaseOneResponses.add(resp)
				if s.metrics.Enabled {
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
		if instruction == PhaseTwoRequest_COMMIT {
			validSNs = append(validSNs, serialNumbers...)
		}
		txIds = append(txIds, txID)
	}

	s.logger.Debugf("shard [%d] committing [%d] serial numbers", s.id, len(validSNs))
	startCommit := time.Now()
	if err := s.db.Commit(validSNs); err != nil {
		panic(err)
	}
	s.pendingCommits.DeleteBatch(txIds)

	if s.metrics.Enabled {
		s.metrics.SNCommitDuration.With(metrics.ShardId(s.id)).Set(float64(time.Now().Sub(startCommit)))
		s.metrics.SNCommitSize.With(metrics.ShardId(s.id)).Set(float64(len(validSNs)))
		s.metrics.CommittedSNs.With(metrics.ShardId(s.id)).Add(float64(len(validSNs)))
		s.metrics.PendingCommitsSNs.Set(s.pendingCommits.CountSNs())
		s.metrics.PendingCommitsTxIds.Set(s.pendingCommits.CountTxs())
	}
}

func (s *shard) accumulatedPhaseOneResponse() []*PhaseOneResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.phaseOneResponses.getAndRemoveAll()
}

func (s *shard) delete() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.wg.Wait()

	s.db.Close()
	s.phaseOneResponses = nil
	return os.RemoveAll(s.path)
}

type phaseOneResponse struct {
	responses []*PhaseOneResponse
	mu        sync.RWMutex
}

func (r *phaseOneResponse) add(resp *PhaseOneResponse) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.responses = append(r.responses, resp)
}

func (r *phaseOneResponse) getAndRemoveAll() []*PhaseOneResponse {
	r.mu.Lock()
	defer r.mu.Unlock()

	responses := r.responses
	r.responses = []*PhaseOneResponse{}
	return responses
}
