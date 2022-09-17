package pipeline

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type dbInflightTxs struct {
	phaseOnePendingTxs       *phaseOnePendingTxs
	phaseOneProcessedTxsChan chan []*phaseOneProcessedTx
}

///////////////////////////////////////  shardsServerMgr  ////////////////////////////////
type shardsServerMgr struct {
	config          *ShardsServerMgrConfig
	shardServers    []*shardsServer
	shardIdToServer map[int]*shardsServer
	numShards       int

	inputChan        chan map[TxSeqNum][][]byte
	twoPCInflightTxs *dbInflightTxs
	outputChan       chan []*TxStatus

	stopSignalCh chan struct{}
}

func newShardsServerMgr(c *ShardsServerMgrConfig) (*shardsServerMgr, error) {
	shardsServerMgr := &shardsServerMgr{
		config:          c,
		shardServers:    []*shardsServer{},
		shardIdToServer: map[int]*shardsServer{},

		twoPCInflightTxs: &dbInflightTxs{
			phaseOnePendingTxs: &phaseOnePendingTxs{
				m: map[TxSeqNum]*phaseOnePendingTx{},
			},
			phaseOneProcessedTxsChan: make(chan []*phaseOneProcessedTx, defaultChannelBufferSize),
		},
		inputChan:    make(chan map[TxSeqNum][][]byte, defaultChannelBufferSize),
		outputChan:   make(chan []*TxStatus, defaultChannelBufferSize),
		stopSignalCh: make(chan struct{}),
	}

	hosts := []string{}
	for a := range c.ShardsServersToNumShards {
		hosts = append(hosts, a)
	}
	sort.Strings(hosts)

	firstShardNum := 0
	for _, a := range hosts {
		lastShardNum := firstShardNum + c.ShardsServersToNumShards[a] - 1

		shardServer, err := newShardServer(a,
			firstShardNum, lastShardNum, c.CleanupShards,
			shardsServerMgr.twoPCInflightTxs,
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
	return int(binary.BigEndian.Uint16(sn[0:2]) % uint16(m.numShards))
}

func (m *shardsServerMgr) startInputRecieverRoutine() {
	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				return
			case txs := <-m.inputChan:
				m.sendPhaseOneMessages(txs)
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
	m.twoPCInflightTxs.phaseOnePendingTxs.add(pendingTxs)

	for server, reqBatch := range reqBatchByServer {
		server.phaseOneComm.sendCh <- reqBatch
	}
}

func (m *shardsServerMgr) startPhaseTwoProcessingRoutine() {

	writeToOutputChansAndSendPhaseTwoMessages := func(phaseOneProcessedTxs []*phaseOneProcessedTx) {
		status := []*TxStatus{}
		phaseTwoMessages := map[*shardsServer]*shardsservice.PhaseTwoRequestBatch{}

		for _, t := range phaseOneProcessedTxs {
			for s := range t.shardServers {
				reqBatch, ok := phaseTwoMessages[s]
				if !ok {
					reqBatch = &shardsservice.PhaseTwoRequestBatch{
						Requests: []*shardsservice.PhaseTwoRequest{},
					}
					phaseTwoMessages[s] = reqBatch
				}
				status = append(status,
					&TxStatus{
						TxSeqNum: t.txSeqNum,
						IsValid:  t.isValid,
					},
				)
				instruction := shardsservice.PhaseTwoRequest_COMMIT
				if !t.isValid {
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
		for s, m := range phaseTwoMessages {
			s.phaseTwoComm.sendCh <- m
		}
	}

	go func() {
		for {
			select {
			case <-m.stopSignalCh:
				close(m.outputChan)
				return
			case t := <-m.twoPCInflightTxs.phaseOneProcessedTxsChan:
				writeToOutputChansAndSendPhaseTwoMessages(t)
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

func newShardServer(host string,
	firstShardNum, lastShardNum int, cleanupShards bool,
	twoPCInflightTxs *dbInflightTxs) (*shardsServer, error) {

	if cleanupShards {
		conn, err := grpc.Dial(
			fmt.Sprintf("%s:%d", host, config.DefaultGRPCPortShardsServer),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)

		if err != nil {
			return nil, err
		}

		client := shardsservice.NewServerClient(conn)

		if _, err := client.DeleteShards(context.Background(), &shardsservice.Empty{}); err != nil {
			return nil, err
		}

		if _, err := client.SetupShards(context.Background(),
			&shardsservice.ShardsSetupRequest{
				FirstShardNum: uint32(firstShardNum),
				LastShardNum:  uint32(lastShardNum),
			},
		); err != nil {
			return nil, err
		}
		if err := conn.Close(); err != nil {
			return nil, err
		}
	}

	o, err := newPhaseOneComm(host)
	if err != nil {
		return nil, err
	}

	t, err := newPhaseTwoComm(host)
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
	stream              shardsservice.Server_StartPhaseOneStreamClient
	streamContext       context.Context
	streamContextCancel func()
	sendCh              chan *shardsservice.PhaseOneRequestBatch

	stopSignalCh chan struct{}
	stopWG       sync.WaitGroup
}

func newPhaseOneComm(host string) (*phaseOneComm, error) {
	conn, err := grpc.Dial(
		fmt.Sprintf("%s:%d", host, config.DefaultGRPCPortShardsServer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := shardsservice.NewServerClient(conn)
	cancelableContext, cancel := context.WithCancel(context.Background())
	stream, err := client.StartPhaseOneStream(cancelableContext)

	if err != nil {
		cancel()
		return nil, err
	}

	return &phaseOneComm{
		stream:              stream,
		streamContext:       cancelableContext,
		streamContextCancel: cancel,
		sendCh:              make(chan *shardsservice.PhaseOneRequestBatch, defaultChannelBufferSize),
		stopSignalCh:        make(chan struct{}),
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
				err := oc.stream.Send(b)
				if err != nil {
					panic(fmt.Sprintf("Error while sending sig verification request batch on stream: %s", err))
				}
			}
		}
	}()
}

func (oc *phaseOneComm) startResponseRecieverRoutine(shardServer *shardsServer, twoPCInflightTxs *dbInflightTxs) {
	phaseOnePendingTxs := twoPCInflightTxs.phaseOnePendingTxs
	phaseOneProcessedTxsChan := twoPCInflightTxs.phaseOneProcessedTxsChan
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

			phaseOneValidationResults := map[TxSeqNum]bool{}
			for _, response := range responseBatch.Responses {
				isValid := false
				if response.Status != shardsservice.PhaseOneResponse_CANNOT_COMMITTED {
					isValid = true
				}
				phaseOneValidationResults[TxSeqNum{
					BlkNum: response.BlockNum,
					TxNum:  response.TxNum,
				}] = isValid
			}
			phaseOneProcessedTxsChan <- phaseOnePendingTxs.processPhaseOneValidationResults(shardServer, phaseOneValidationResults)
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
	stream       shardsservice.Server_StartPhaseTwoStreamClient
	sendCh       chan *shardsservice.PhaseTwoRequestBatch
	stopSignalCh chan struct{}
	stopWG       sync.WaitGroup
}

func newPhaseTwoComm(host string) (*phaseTwoComm, error) {
	conn, err := grpc.Dial(
		fmt.Sprintf("%s:%d", host, config.DefaultGRPCPortShardsServer),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}

	client := shardsservice.NewServerClient(conn)
	stream, err := client.StartPhaseTwoStream(context.Background())

	if err != nil {
		panic(fmt.Sprintf("Error while starting phase two stream: %s", err))
	}

	return &phaseTwoComm{
		stream:       stream,
		sendCh:       make(chan *shardsservice.PhaseTwoRequestBatch, defaultChannelBufferSize),
		stopSignalCh: make(chan struct{}),
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
				err := tc.stream.Send(b)
				if err != nil {
					panic(fmt.Sprintf("Error while sending sig verification request batch on stream: %s", err))
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
	shardServer *shardsServer, validationStatus map[TxSeqNum]bool) []*phaseOneProcessedTx {

	t.mu.Lock()
	defer t.mu.Unlock()
	phaseOneProcessedTxs := []*phaseOneProcessedTx{}

	for txSeqNum, isValid := range validationStatus {
		pendingTx := t.m[txSeqNum]
		delete(t.m, txSeqNum)
		processedTx := &phaseOneProcessedTx{
			txSeqNum:     txSeqNum,
			shardServers: pendingTx.shardServers,
			isValid:      isValid,
		}
		if !isValid {
			processedTx.invalidatedBy = shardServer
		}
		phaseOneProcessedTxs = append(phaseOneProcessedTxs, processedTx)
	}

	return phaseOneProcessedTxs
}

type phaseOneProcessedTx struct {
	txSeqNum      TxSeqNum
	shardServers  map[*shardsServer]struct{}
	isValid       bool
	invalidatedBy *shardsServer
}
