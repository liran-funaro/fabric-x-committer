package pipeline

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type dbInflightTxs struct {
	phaseOnePendingTxs       *phaseOnePendingTxs
	phaseOneProcessedTxsChan chan []*phaseOneProcessedTx
}

///////////////////////////////////////  shardsServerMgr  ////////////////////////////////
type shardsServerMgr struct {
	shardServers    []*shardsServer
	shardIdToServer map[int]*shardsServer
	numShards       int

	inputChan                     chan map[TxSeqNum][][]byte
	inflightTxs                   *dbInflightTxs
	outputChan                    chan []*TxStatus
	prefixSizeForShardCalculation int

	stopSignalCh chan struct{}
	metrics      *metrics.Metrics
}

func newShardsServerMgr(config *ShardsServerMgrConfig, metrics *metrics.Metrics) (*shardsServerMgr, error) {
	shardsServerMgr := &shardsServerMgr{
		shardServers:    []*shardsServer{},
		shardIdToServer: map[int]*shardsServer{},

		inflightTxs: &dbInflightTxs{
			phaseOnePendingTxs: &phaseOnePendingTxs{
				m: map[TxSeqNum]*phaseOnePendingTx{},
			},
			phaseOneProcessedTxsChan: make(chan []*phaseOneProcessedTx, defaultChannelBufferSize),
		},
		inputChan:                     make(chan map[TxSeqNum][][]byte, defaultChannelBufferSize),
		outputChan:                    make(chan []*TxStatus, defaultChannelBufferSize),
		prefixSizeForShardCalculation: config.PrefixSizeForShardCalculation,
		stopSignalCh:                  make(chan struct{}),
		metrics:                       metrics,
	}
	if metrics.Enabled {
		metrics.PhaseOneProcessedChLength.SetCapacity(defaultChannelBufferSize)
		metrics.ShardMgrInputChLength.SetCapacity(defaultChannelBufferSize)
		metrics.ShardMgrOutputChLength.SetCapacity(defaultChannelBufferSize)
	}
	config.SortServers()

	firstShardNum := 0
	for _, server := range config.Servers {
		lastShardNum := firstShardNum + server.NumShards - 1

		shardServer, err := newShardServer(
			server.Endpoint,
			firstShardNum, lastShardNum,
			config.DeleteExistingShards,
			shardsServerMgr.inflightTxs,
			metrics,
		)
		if err != nil {
			return nil, err
		}

		shardsServerMgr.shardServers = append(shardsServerMgr.shardServers, shardServer)
		for i := firstShardNum; i <= lastShardNum; i++ {
			shardsServerMgr.shardIdToServer[i] = shardServer
		}
		firstShardNum = lastShardNum + 1
	}

	shardsServerMgr.numShards = firstShardNum
	shardsServerMgr.startInputRecieverRoutine()
	shardsServerMgr.startPhaseTwoProcessingRoutine()

	return shardsServerMgr, nil
}

func (m *shardsServerMgr) computeShardId(sn []byte) int {
	return int(binary.BigEndian.Uint16(sn[0:m.prefixSizeForShardCalculation]) % uint16(m.numShards))
}

func (m *shardsServerMgr) startInputRecieverRoutine() {
	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				return
			case txs := <-m.inputChan:
				m.sendPhaseOneMessages(txs)
				if m.metrics.Enabled {
					m.metrics.ShardMgrInTxs.Add(len(txs))
					m.metrics.ShardMgrInputChLength.Set(len(m.inputChan))
				}
			}
		}
	}()
}

func (m *shardsServerMgr) sendPhaseOneMessages(txs map[TxSeqNum][][]byte) {
	reqBatchByServer := map[*shardsServer]*shardsservice.PhaseOneRequestBatch{}
	pendingTxs := map[TxSeqNum]*phaseOnePendingTx{}

	for txSeq, sns := range txs {
		reqByServer := map[*shardsServer]*shardsservice.PhaseOneRequest{}
		for _, sn := range sns {
			shardID := m.computeShardId(sn)
			shardServer := m.shardIdToServer[shardID]

			req, ok := reqByServer[shardServer]
			if !ok {
				req = &shardsservice.PhaseOneRequest{
					BlockNum:               txSeq.BlkNum,
					TxNum:                  txSeq.TxNum,
					ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{},
				}
				reqByServer[shardServer] = req
			}

			serialNumsForServer, ok := req.ShardidToSerialNumbers[uint32(shardID)]
			if !ok {
				serialNumsForServer = &shardsservice.SerialNumbers{}
				req.ShardidToSerialNumbers[uint32(shardID)] = serialNumsForServer
			}
			serialNumsForServer.SerialNumbers = append(serialNumsForServer.SerialNumbers, sn)
		}

		if len(reqByServer) == 1 {
			for _, req := range reqByServer {
				req.IsCompleteTx = true
			}
		}

		pendingTx := &phaseOnePendingTx{
			shardServers:        map[*shardsServer]struct{}{},
			pendingResponseNums: len(reqByServer),
		}

		for shardServer, req := range reqByServer {
			batch, ok := reqBatchByServer[shardServer]
			if !ok {
				batch = &shardsservice.PhaseOneRequestBatch{}
				reqBatchByServer[shardServer] = batch
			}
			batch.Requests = append(batch.Requests, req)
			pendingTx.shardServers[shardServer] = struct{}{}
		}
		pendingTxs[txSeq] = pendingTx
	}
	m.inflightTxs.phaseOnePendingTxs.add(pendingTxs)

	for server, reqBatch := range reqBatchByServer {
		server.phaseOneComm.sendCh <- reqBatch
		if m.metrics.Enabled {
			m.metrics.PhaseOneInTxs.Add(len(reqBatch.Requests))
			m.metrics.PhaseOneSendChLength.Set(len(server.phaseOneComm.sendCh))
		}
	}
}

func (m *shardsServerMgr) startPhaseTwoProcessingRoutine() {

	writeToOutputChansAndSendPhaseTwoMessages := func(phaseOneProcessedTxs []*phaseOneProcessedTx) {
		logger.Debugf("Returned batch of %d statuses from shards service P1.", len(phaseOneProcessedTxs))
		status := []*TxStatus{}
		phaseTwoMessages := map[*shardsServer]*shardsservice.PhaseTwoRequestBatch{}

		for _, t := range phaseOneProcessedTxs {
			status = append(status,
				&TxStatus{
					TxSeqNum: t.txSeqNum,
					Status:   t.status,
				},
			)

			for s := range t.shardServers {
				reqBatch, ok := phaseTwoMessages[s]
				if !ok {
					reqBatch = &shardsservice.PhaseTwoRequestBatch{
						Requests: []*shardsservice.PhaseTwoRequest{},
					}
					phaseTwoMessages[s] = reqBatch
				}
				instruction := shardsservice.PhaseTwoRequest_COMMIT
				if t.status != VALID {
					instruction = shardsservice.PhaseTwoRequest_FORGET
				}
				reqBatch.Requests = append(reqBatch.Requests,
					&shardsservice.PhaseTwoRequest{
						BlockNum:    t.txSeqNum.BlkNum,
						TxNum:       t.txSeqNum.TxNum,
						Instruction: instruction,
					})
			}
		}

		m.outputChan <- status
		for s, msg := range phaseTwoMessages {
			s.phaseTwoComm.sendCh <- msg
			if m.metrics.Enabled {
				m.metrics.PhaseTwoOutTxs.Add(len(msg.Requests))
				m.metrics.PhaseTwoSendChLength.Set(len(s.phaseTwoComm.sendCh))
			}
		}
		if m.metrics.Enabled {
			m.metrics.ShardMgrOutTxs.Add(len(status))
			m.metrics.ShardMgrOutputChLength.Set(len(m.outputChan))
		}
	}

	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				close(m.outputChan)
				return
			case t := <-m.inflightTxs.phaseOneProcessedTxsChan:
				writeToOutputChansAndSendPhaseTwoMessages(t)
				if m.metrics.Enabled {
					m.metrics.PhaseOneProcessedChLength.Set(len(m.inflightTxs.phaseOneProcessedTxsChan))
				}
			}
		}
	}()
}

func (m *shardsServerMgr) stop() {
	for _, s := range m.shardServers {
		s.stop()
	}
	close(m.stopSignalCh)
}

///////////////////////////////////////  shardsServer  ////////////////////////////////
type shardsServer struct {
	phaseOneComm *phaseOneComm
	phaseTwoComm *phaseTwoComm
}

func newShardServer(endpoint *connection.Endpoint,
	firstShardNum, lastShardNum int,
	cleanupShards bool,
	twoPCInflightTxs *dbInflightTxs, metrics *metrics.Metrics) (*shardsServer, error) {

	if cleanupShards {
		conn, err := connection.Connect(connection.NewDialConfig(*endpoint))
		if err != nil {
			return nil, err
		}

		client := shardsservice.NewShardsClient(conn)
		if _, err := client.DeleteShards(context.Background(), &shardsservice.Empty{}); err != nil {
			return nil, err
		}

		if _, err := client.SetupShards(context.Background(),
			&shardsservice.ShardsSetupRequest{
				FirstShardId: uint32(firstShardNum),
				LastShardId:  uint32(lastShardNum),
			},
		); err != nil {
			return nil, err
		}
		if err := conn.Close(); err != nil {
			return nil, err
		}
	}

	o, err := newPhaseOneComm(endpoint.Address(), metrics)
	if err != nil {
		return nil, err
	}

	t, err := newPhaseTwoComm(endpoint.Address(), metrics)
	if err != nil {
		return nil, err
	}

	s := &shardsServer{
		phaseOneComm: o,
		phaseTwoComm: t,
	}
	o.startRequestSenderRoutine()
	o.startResponseRecieverRoutine(s, twoPCInflightTxs)
	t.startRequestSenderRoutine()
	return s, nil
}

func (s *shardsServer) stop() {
	s.phaseOneComm.stopRequestSenderRoutine()
	s.phaseOneComm.stopResponseRecieverRoutine()
	s.phaseTwoComm.stopRequestSenderRoutine()
}

//////////////////////////////////// phaseOneComm ////////////////////////////////////
type phaseOneComm struct {
	stream              shardsservice.Shards_StartPhaseOneStreamClient
	streamContext       context.Context
	streamContextCancel func()
	sendCh              chan *shardsservice.PhaseOneRequestBatch

	stopSignalCh chan struct{}
	stopWG       sync.WaitGroup
	metrics      *metrics.Metrics
}

func newPhaseOneComm(address string, metrics *metrics.Metrics) (*phaseOneComm, error) {
	conn, err := grpc.Dial(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := shardsservice.NewShardsClient(conn)
	cancelableContext, cancel := context.WithCancel(context.Background())
	stream, err := client.StartPhaseOneStream(cancelableContext)

	if err != nil {
		cancel()
		return nil, err
	}

	if metrics.Enabled {
		metrics.PhaseOneSendChLength.SetCapacity(defaultChannelBufferSize)
	}
	return &phaseOneComm{
		stream:              stream,
		streamContext:       cancelableContext,
		streamContextCancel: cancel,
		sendCh:              make(chan *shardsservice.PhaseOneRequestBatch, defaultChannelBufferSize),
		stopSignalCh:        make(chan struct{}),
		metrics:             metrics,
	}, nil
}

func (oc *phaseOneComm) startRequestSenderRoutine() {
	go func() {
		for {
			select {
			case <-oc.stopSignalCh:
				err := oc.stream.CloseSend()
				if err != nil {
					panic(fmt.Sprintf("Error while closing the stream for sending: %s", err))
				}
				oc.stopWG.Done()
				return
			case b := <-oc.sendCh:
				logger.Debugf("Sending %v TXs to shards service P1.", len(b.Requests))
				err := oc.stream.Send(b)
				if err != nil {
					panic(fmt.Sprintf("Error while sending sig verification request batch on stream: %s", err))
				}
				if oc.metrics.Enabled {
					oc.metrics.PhaseOneSendChLength.Set(len(oc.sendCh))
				}
			}
		}
	}()
}

func (oc *phaseOneComm) startResponseRecieverRoutine(shardServer *shardsServer, inflightTxs *dbInflightTxs) {
	phaseOnePendingTxs := inflightTxs.phaseOnePendingTxs
	phaseOneProcessedTxsChan := inflightTxs.phaseOneProcessedTxsChan
	go func() {
		for {
			responseBatch, err := oc.stream.Recv()
			if oc.streamContext.Err() == context.Canceled {
				oc.stopWG.Done()
				return
			}

			if err != nil {
				panic(fmt.Sprintf("Error while reaading sig verification response batch on stream: %s", err))
			}

			phaseOneValidationResults := map[TxSeqNum]Status{}
			for _, response := range responseBatch.Responses {
				status := DOUBLE_SPEND
				if response.Status != shardsservice.PhaseOneResponse_CANNOT_COMMITTED {
					status = VALID
				}
				phaseOneValidationResults[TxSeqNum{
					BlkNum: response.BlockNum,
					TxNum:  response.TxNum,
				}] = status
			}
			phaseOneProcessedTxs := phaseOnePendingTxs.processPhaseOneValidationResults(shardServer, phaseOneValidationResults)
			if len(phaseOneProcessedTxs) > 0 {
				phaseOneProcessedTxsChan <- phaseOneProcessedTxs
				if oc.metrics.Enabled {
					oc.metrics.PhaseOneOutTxs.Add(len(phaseOneProcessedTxs))
					oc.metrics.PhaseOneProcessedChLength.Set(len(phaseOneProcessedTxsChan))
				}
			}
		}
	}()
}

func (oc *phaseOneComm) stopRequestSenderRoutine() {
	oc.stopWG.Add(1)
	oc.stopSignalCh <- struct{}{}
	oc.stopWG.Wait()
}

func (oc *phaseOneComm) stopResponseRecieverRoutine() {
	oc.stopWG.Add(1)
	oc.streamContextCancel()
	oc.stopWG.Wait()
}

//////////////////////////////////// phaseTwoComm ////////////////////////////////////
type phaseTwoComm struct {
	stream       shardsservice.Shards_StartPhaseTwoStreamClient
	sendCh       chan *shardsservice.PhaseTwoRequestBatch
	stopSignalCh chan struct{}
	stopWG       sync.WaitGroup
	metrics      *metrics.Metrics
}

func newPhaseTwoComm(address string, metrics *metrics.Metrics) (*phaseTwoComm, error) {
	conn, err := grpc.Dial(
		address,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := shardsservice.NewShardsClient(conn)
	stream, err := client.StartPhaseTwoStream(context.Background())

	if err != nil {
		panic(fmt.Sprintf("Error while starting phase two stream: %s", err))
	}

	if metrics.Enabled {
		metrics.PhaseTwoSendChLength.SetCapacity(defaultChannelBufferSize)
	}
	return &phaseTwoComm{
		stream:       stream,
		sendCh:       make(chan *shardsservice.PhaseTwoRequestBatch, defaultChannelBufferSize),
		stopSignalCh: make(chan struct{}),
		metrics:      metrics,
	}, nil
}

func (tc *phaseTwoComm) startRequestSenderRoutine() {
	go func() {
		for {
			select {
			case <-tc.stopSignalCh:
				err := tc.stream.CloseSend()
				if err != nil {
					panic(fmt.Sprintf("Error while closing the stream for sending: %s", err))
				}
				tc.stopWG.Done()
				return
			case b := <-tc.sendCh:
				logger.Debugf("Sending %v TXs to shards service P2.", len(b.Requests))
				err := tc.stream.Send(b)
				if err != nil {
					panic(fmt.Sprintf("Error while sending sig verification request batch on stream: %s", err))
				}
				if tc.metrics.Enabled {
					tc.metrics.PhaseTwoInTxs.Add(len(b.Requests))
					tc.metrics.PhaseTwoSendChLength.Set(len(tc.sendCh))
				}
			}
		}
	}()
}

func (o *phaseTwoComm) stopRequestSenderRoutine() {
	o.stopWG.Add(1)
	o.stopSignalCh <- struct{}{}
	o.stopWG.Wait()
}

type phaseOnePendingTx struct {
	shardServers        map[*shardsServer]struct{}
	pendingResponseNums int
	invalidatedBy       *shardsServer
}

type phaseOnePendingTxs struct {
	mu sync.Mutex
	m  map[TxSeqNum]*phaseOnePendingTx
}

func (t *phaseOnePendingTxs) add(txs map[TxSeqNum]*phaseOnePendingTx) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for txSeqNum, pendingTx := range txs {
		t.m[txSeqNum] = pendingTx
	}
}

func (t *phaseOnePendingTxs) processPhaseOneValidationResults(
	shardServer *shardsServer, validationStatus map[TxSeqNum]Status) []*phaseOneProcessedTx {

	t.mu.Lock()
	defer t.mu.Unlock()
	phaseOneProcessedTxs := []*phaseOneProcessedTx{}

	for txSeqNum, status := range validationStatus {
		pendingTx := t.m[txSeqNum]

		if status != VALID {
			pendingTx.invalidatedBy = shardServer
		}

		pendingTx.pendingResponseNums--
		if pendingTx.pendingResponseNums > 0 {
			continue
		}

		delete(t.m, txSeqNum)
		processedTx := &phaseOneProcessedTx{
			txSeqNum:      txSeqNum,
			shardServers:  pendingTx.shardServers,
			invalidatedBy: pendingTx.invalidatedBy,
			status:        status,
		}

		phaseOneProcessedTxs = append(phaseOneProcessedTxs, processedTx)
	}

	return phaseOneProcessedTxs
}

type phaseOneProcessedTx struct {
	txSeqNum      TxSeqNum
	shardServers  map[*shardsServer]struct{}
	status        Status
	invalidatedBy *shardsServer
}
