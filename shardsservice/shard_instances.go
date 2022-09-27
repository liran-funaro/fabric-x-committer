package shardsservice

import (
	"io/ioutil"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

type shardInstances struct {
	shardIDToInstance     *shardIDToInstances
	txIDToShardID         *txIDToShardID
	txIDToPendingResponse *txIDToPendingResponse
	rootDir               string
	mu                    sync.RWMutex
	phaseOneResponses     chan []*PhaseOneResponse
	logger                *logging.Logger
}

func newShardInstances(phaseOneResponse chan []*PhaseOneResponse, rootDir string) (*shardInstances, error) {
	logger := logging.New("shard instances")
	logger.Info("Initializing shard instances manager")

	si := &shardInstances{
		shardIDToInstance:     &shardIDToInstances{idToShard: make(map[uint32]*shard)},
		txIDToShardID:         &txIDToShardID{txToShardID: make(map[txID][]uint32)},
		txIDToPendingResponse: &txIDToPendingResponse{tIDToPendingShardIDResp: make(txIDToPendingShardIDResponse)},
		rootDir:               rootDir,
		mu:                    sync.RWMutex{},
		phaseOneResponses:     phaseOneResponse,
		logger:                logger,
	}

	// TODO: Use a db to keep track of existing shards and
	// shard requests to handle failure and recovery.
	// The below part of the code needs to be rewritten for
	// production environment
	files, err := ioutil.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	r, err := regexp.Compile("shard_([0-9])+")
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if r.MatchString(f.Name()) {
			shardID, err := getShardID(f.Name())
			if err != nil {
				return nil, err
			}
			if err := si.setup(uint32(shardID)); err != nil {
				return nil, err
			}
		}
	}

	return si, nil
}

func (i *shardInstances) setup(shardID uint32) error {
	path := shardFilePath(i.rootDir, shardID)
	shard, err := newShard(shardID, path)
	if err != nil {
		return err
	}

	i.shardIDToInstance.addShard(shardID, shard)
	return nil
}

func (i *shardInstances) deleteAll() error {
	i.mu.Lock()
	defer i.mu.Unlock()

	// TODO: need to handle failure and recovery
	shards := i.shardIDToInstance.getAllShards()

	for _, s := range shards {
		if err := s.delete(); err != nil {
			return err
		}
	}
	i.shardIDToInstance.deleteAllShards()

	return nil
}

func (i *shardInstances) executePhaseOne(requests *PhaseOneRequestBatch) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	// 1. Transactions are grouped by shards on which it needs to execute
	shardToTxSn := make(shardIDToTxIDSerialNumbers)

	for _, r := range requests.Requests {
		for shardID, serialNumbers := range r.GetShardidToSerialNumbers() {
			txToSn, ok := shardToTxSn[shardID]
			if !ok {
				txToSn = make(txIDToSerialNumbers)
				shardToTxSn[shardID] = txToSn
			}

			tID := txID{
				blockNum: r.BlockNum,
				txNum:    r.TxNum,
			}

			sn, ok := txToSn[tID]
			if !ok {
				sn = &SerialNumbers{}
				txToSn[tID] = sn
			}

			i.logger.Debugf("adding [%d] serial numbers to txID [%v]", len(serialNumbers.SerialNumbers), tID)
			sn.SerialNumbers = append(sn.SerialNumbers, serialNumbers.SerialNumbers...)

			i.logger.Debugf("assigning txID [%v] to shardID [%d]", tID, shardID)
			i.txIDToShardID.add(tID, shardID)
			i.txIDToPendingResponse.add(tID, shardID)
		}
	}

	// 2. Grouped transactions are submitted to each shard
	for shardID, txToSn := range shardToTxSn {
		shard := i.shardIDToInstance.getShard(shardID)
		i.logger.Debugf("validating transactions on shardID [%d]", shardID)

		go func(txToSn txIDToSerialNumbers) {
			shard.executePhaseOne(txToSn)
		}(txToSn)
	}
}

func (i *shardInstances) executePhaseTwo(requests *PhaseTwoRequestBatch) {
	i.mu.RLock()
	defer i.mu.RUnlock()

	// 1. Transaction instructions are grouped by shardID
	shardToTxIns := make(shardIDToTxIDInstruction)

	for _, r := range requests.Requests {
		tID := txID{
			blockNum: r.BlockNum,
			txNum:    r.TxNum,
		}

		shardIDs := i.txIDToShardID.get(tID)

		for _, shardID := range shardIDs {
			txToIns, ok := shardToTxIns[shardID]
			if !ok {
				txToIns = make(txIDToInstruction)
				shardToTxIns[shardID] = txToIns
			}

			txToIns[tID] = r.Instruction
		}
	}

	// 2. Grouped instructions are submitted to associated shard
	for shardID, txToIns := range shardToTxIns {
		shard := i.shardIDToInstance.getShard(shardID)

		go func(txToIns txIDToInstruction) {
			shard.executePhaseTwo(txToIns)
		}(txToIns)
	}
}

func (i *shardInstances) accumulatedPhaseOneResponses(maxBatchItemCount uint32, batchCutTimeout time.Duration) {
	ticker := time.NewTicker(batchCutTimeout)

	var responses []*PhaseOneResponse

	for {
		select {
		case <-ticker.C:
			if len(responses) > 0 {
				i.logger.Debug("emitting response due to timeout")
				i.phaseOneResponses <- responses
				responses = nil
			}
		default:
			i.mu.RLock()
			shards := i.shardIDToInstance.getAllShards()
			for _, s := range shards {
				if resp := s.accumulatedPhaseOneResponse(); resp != nil {
					for _, r := range resp {
						tID := txID{
							blockNum: r.BlockNum,
							txNum:    r.TxNum,
						}
						if r.Status == PhaseOneResponse_CAN_COMMIT {
							noMorePendingResponse, isNotTracked := i.txIDToPendingResponse.removeDueToValid(tID, s.id)
							if isNotTracked {
								continue
							}

							if noMorePendingResponse {
								responses = append(responses, r)
							}
						} else if r.Status == PhaseOneResponse_CANNOT_COMMITTED {
							existRemoved := i.txIDToPendingResponse.removeDueToInvalid(tID)
							if existRemoved {
								responses = append(responses, r)
							}
						}
					}
				}
			}
			i.mu.RUnlock()

			if uint32(len(responses)) >= maxBatchItemCount {
				i.logger.Debug("emitting response due to max batch size")
				i.phaseOneResponses <- responses[:maxBatchItemCount]
				responses = responses[maxBatchItemCount:]
			}
		}
	}
}

type shardIDToInstances struct {
	idToShard map[uint32]*shard
	mu        sync.RWMutex
}

func (i *shardIDToInstances) addShard(shardID uint32, s *shard) {
	i.mu.Lock()
	defer i.mu.Unlock()

	i.idToShard[shardID] = s
}

func (i *shardIDToInstances) getShard(shardID uint32) *shard {
	i.mu.RLock()
	defer i.mu.RUnlock()

	return i.idToShard[shardID]
}

func (i *shardIDToInstances) getAllShards() []*shard {
	i.mu.RLock()
	defer i.mu.RUnlock()

	var shards []*shard
	for _, s := range i.idToShard {
		shards = append(shards, s)
	}

	return shards
}

func (i *shardIDToInstances) deleteAllShards() {
	i.mu.Lock()
	defer i.mu.Unlock()

	for id := range i.idToShard {
		delete(i.idToShard, id)
	}
}

type txIDToShardID struct {
	txToShardID map[txID][]uint32
	mu          sync.RWMutex
}

func (t *txIDToShardID) add(tID txID, shardID uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.txToShardID[tID] = append(t.txToShardID[tID], shardID)
}

func (t *txIDToShardID) get(tID txID) []uint32 {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.txToShardID[tID]
}

func shardFilePath(rootDir string, shardID uint32) string {
	return path.Join(rootDir, "shard_"+strconv.FormatUint(uint64(shardID), 10))
}

func getShardID(name string) (uint64, error) {
	splitedStr := strings.Split(name, "_")
	return strconv.ParseUint(splitedStr[1], 10, 64)
}

type txIDToPendingResponse struct {
	tIDToPendingShardIDResp txIDToPendingShardIDResponse
	mu                      sync.RWMutex
}

func (t *txIDToPendingResponse) add(tID txID, shardID uint32) {
	t.mu.Lock()
	defer t.mu.Unlock()

	shardIDResp, ok := t.tIDToPendingShardIDResp[tID]
	if !ok {
		shardIDResp = make(pendingShardIDResponse)
		t.tIDToPendingShardIDResp[tID] = shardIDResp
	}

	shardIDResp[shardID] = struct{}{}
}

func (t *txIDToPendingResponse) removeDueToInvalid(tID txID) (existRemoved bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.tIDToPendingShardIDResp[tID]; !ok {
		existRemoved = false
		return
	}
	delete(t.tIDToPendingShardIDResp, tID)
	existRemoved = true
	return
}

func (t *txIDToPendingResponse) removeDueToValid(tID txID, shardID uint32) (noMorePendingResponse bool, isNotTracked bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	shardIDResp, ok := t.tIDToPendingShardIDResp[tID]
	if !ok {
		// means the tx might be invalid and hence, the pending responses are not tracked
		isNotTracked = true
	}

	delete(shardIDResp, shardID)
	noMorePendingResponse = len(shardIDResp) == 0

	return
}

type shardIDToTxIDSerialNumbers map[uint32]txIDToSerialNumbers

type txIDToSerialNumbers map[txID]*SerialNumbers

type txID struct {
	blockNum uint64
	txNum    uint64
}

type shardIDToTxIDInstruction map[uint32]txIDToInstruction

type txIDToInstruction map[txID]PhaseTwoRequest_Instruction

type txIDToPendingShardIDResponse map[txID]pendingShardIDResponse

type pendingShardIDResponse map[uint32]struct{}
