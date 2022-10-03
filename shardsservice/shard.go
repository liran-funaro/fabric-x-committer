package shardsservice

import (
	"os"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/goleveldb"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type shard struct {
	id   uint32
	path string
	db   database
	mu   sync.RWMutex

	pendingCommits    *pendingCommits
	phaseOneResponses *phaseOneResponse
	wg                sync.WaitGroup
	logger            *logging.Logger
	metrics           *metrics.Metrics
}

type database interface {
	DoNotExist(keys [][]byte) ([]bool, error)
	Commit(keys [][]byte) error
	Close()
}

func newShard(id uint32, path string, metrics *metrics.Metrics) (*shard, error) {
	//db, err := rocksdb.Open(path)
	db, err := goleveldb.Open(path)
	if err != nil {
		return nil, err
	}

	return &shard{
		id:                id,
		path:              path,
		db:                db,
		mu:                sync.RWMutex{},
		pendingCommits:    newPendingCommits(),
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
		go func(tID txID, serialNumbers *SerialNumbers) {
			s.logger.Debugf("shardID [%d] validating txID [%v]", s.id, tID)
			defer s.wg.Done()

			resp := &PhaseOneResponse{
				BlockNum: tID.blockNum,
				TxNum:    tID.txNum,
			}

			s.pendingCommits.waitTillNotExist(serialNumbers.GetSerialNumbers())

			doNoExist, _ := s.db.DoNotExist(serialNumbers.GetSerialNumbers())
			for _, notExist := range doNoExist {
				if !notExist {
					s.logger.Debugf("shardID [%d] invalidates txID [%v]", s.id, tID)
					resp.Status = PhaseOneResponse_CANNOT_COMMITTED
					s.phaseOneResponses.add(resp)
					return
				}
			}

			resp.Status = PhaseOneResponse_CAN_COMMIT
			s.pendingCommits.add(tID, serialNumbers)
			s.phaseOneResponses.add(resp)
			s.logger.Debugf("shardID [%d] successfully validated txID [%v]", s.id, tID)
		}(tID, serialNumbers)
	}
}

func (s *shard) executePhaseTwo(requests txIDToInstruction) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sNumbers := &SerialNumbers{}

	for txID, instruction := range requests {
		// TODO: commit in progress should be recorded for recovery
		serialNumbers := s.pendingCommits.get(txID)
		if instruction == PhaseTwoRequest_COMMIT {
			sNumbers.SerialNumbers = append(sNumbers.SerialNumbers, serialNumbers.SerialNumbers...)
		}
	}

	s.logger.Debugf("shard [%d] committing [%d] serial numbers", s.id, len(sNumbers.SerialNumbers))
	startCommit := time.Now()
	if err := s.db.Commit(sNumbers.SerialNumbers); err != nil {
		panic(err)
	}
	if s.metrics.Enabled {
		s.metrics.SNCommitDuration.With(metrics.ShardId(s.id)).Set(float64(time.Now().Sub(startCommit)/time.Second) / float64(len(sNumbers.SerialNumbers)))
		s.metrics.CommittedSNs.With(metrics.ShardId(s.id)).Add(float64(len(sNumbers.SerialNumbers)))
	}

	for txID := range requests {
		s.pendingCommits.delete(txID)
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
