package pipeline

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
	"time"

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
	numShards       uint16

	twoPCInflightTxs *dbInflightTxs
	outputChan       chan []*txStatus

	stopSignalCh chan struct{}
	stopWg       sync.WaitGroup
}

func newShardsServerMgr(c *ShardsServerMgrConfig) (*shardsServerMgr, error) {
	shardsServerMgr := &shardsServerMgr{
		config:          c,
		shardServers:    []*shardsServer{},
		shardIdToServer: map[int]*shardsServer{},

		twoPCInflightTxs: &dbInflightTxs{
			phaseOnePendingTxs: &phaseOnePendingTxs{
				m: map[txSeqNum]*phaseOnePendingTx{},
			},
			phaseOneProcessedTxsChan: make(chan []*phaseOneProcessedTx, defaultChannelBufferSize),
		},

		outputChan:   make(chan []*txStatus, defaultChannelBufferSize),
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

	shardsServerMgr.numShards = uint16(firstShardNum - 1)
	shardsServerMgr.startPhaseTwoProcessingRoutine()

	return shardsServerMgr, nil
}

func (m *shardsServerMgr) computeShardId(sn []byte) int {
	return int(binary.BigEndian.Uint16(sn[0:2]) % m.numShards)
}

func (m *shardsServerMgr) validateAndCommitAsync(txs map[txSeqNum][][]byte) {
	m.sendPhaseOneMessages(txs)
}

func (m *shardsServerMgr) sendPhaseOneMessages(txs map[txSeqNum][][]byte) {
	reqBatchByServer := map[*shardsServer]*shardsservice.PhaseOneRequestBatch{}
	pendingTxs := map[txSeqNum]*phaseOnePendingTx{}

	for txSeq, sns := range txs {
		reqByServer := map[*shardsServer]*shardsservice.PhaseOneRequest{}
		for _, sn := range sns {
			shardID := m.computeShardId(sn)
			shardServer := m.shardIdToServer[shardID]

			req, ok := reqByServer[shardServer]
			if !ok {
				req = &shardsservice.PhaseOneRequest{
					BlockNum: txSeq.blkNum,
					TxNum:    txSeq.txNum,
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
	timeoutMillis := time.Duration(m.config.BatchConfig.TimeoutMillis * int(time.Millisecond))
	batchSize := m.config.BatchConfig.BatchSize
	phaseOneProcessedTxs := []*phaseOneProcessedTx{}

	writeToOutputChansAndSendPhaseTwoMessages := func() {
		status := []*txStatus{}
		phaseTwoMessages := map[*shardsServer]*shardsservice.PhaseTwoRequestBatch{}

		size := len(phaseOneProcessedTxs)
		if size == 0 {
			return
		}
		if size > batchSize {
			size = batchSize
		}
		b := phaseOneProcessedTxs[:size]
		phaseOneProcessedTxs = phaseOneProcessedTxs[size:]

		for _, t := range b {
			for s := range t.shardServers {
				reqBatch, ok := phaseTwoMessages[s]
				if !ok {
					reqBatch = &shardsservice.PhaseTwoRequestBatch{
						Requests: []*shardsservice.PhaseTwoRequest{},
					}
					phaseTwoMessages[s] = reqBatch
				}
				status = append(status,
					&txStatus{
						txSeqNum: t.txSeqNum,
						isValid:  t.isValid,
					},
				)
				instruction := shardsservice.PhaseTwoRequest_COMMIT
				if !t.isValid {
					instruction = shardsservice.PhaseTwoRequest_FORGET
				}
				reqBatch.Requests = append(reqBatch.Requests,
					&shardsservice.PhaseTwoRequest{
						BlockNum:    t.txSeqNum.blkNum,
						TxNum:       t.txSeqNum.txNum,
						Instruction: instruction,
					})
			}
		}

		for s, m := range phaseTwoMessages {
			s.phaseTwoComm.sendCh <- m
		}

		m.outputChan <- status
	}

	for {
		select {
		case <-m.stopSignalCh:
			for len(phaseOneProcessedTxs) > 0 {
				writeToOutputChansAndSendPhaseTwoMessages()
			}
			m.stopWg.Done()
			return
		case t := <-m.twoPCInflightTxs.phaseOneProcessedTxsChan:
			phaseOneProcessedTxs = append(phaseOneProcessedTxs, t...)
			if len(phaseOneProcessedTxs) >= batchSize {
				writeToOutputChansAndSendPhaseTwoMessages()
			}
		case <-time.After(timeoutMillis):
			writeToOutputChansAndSendPhaseTwoMessages()
		}
	}
}

func (m *shardsServerMgr) stop() {
	for _, s := range m.shardServers {
		s.stop()
	}
	m.stopPhaseTwoProcessingRoutine()
}

func (m *shardsServerMgr) stopPhaseTwoProcessingRoutine() {
	m.stopWg.Add(1)
	m.stopSignalCh <- struct{}{}
	m.stopWg.Wait()
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

			phaseOneValidationResults := map[txSeqNum]bool{}
			for _, response := range responseBatch.Responses {
				isValid := false
				if response.Status != shardsservice.PhaseOneResponse_CANNOT_COMMITTED {
					isValid = true
				}
				phaseOneValidationResults[txSeqNum{
					blkNum: response.BlockNum,
					txNum:  response.TxNum,
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
	stream shardsservice.Server_StartPhaseTwoStreamClient
	sendCh chan *shardsservice.PhaseTwoRequestBatch

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
	cancelableContext, cancel := context.WithCancel(context.Background())
	stream, err := client.StartPhaseTwoStream(cancelableContext)

	if err != nil {
		cancel()
		return nil, err
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
	invalidatedBy       *shardsServer
}

type phaseOnePendingTxs struct {
	mu sync.Mutex
	m  map[txSeqNum]*phaseOnePendingTx
}

func (t *phaseOnePendingTxs) add(txs map[txSeqNum]*phaseOnePendingTx) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for txSeqNum, pendingTx := range txs {
		t.m[txSeqNum] = pendingTx
	}
}

func (t *phaseOnePendingTxs) processPhaseOneValidationResults(
	shardServer *shardsServer, validationStatus map[txSeqNum]bool) []*phaseOneProcessedTx {

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
	txSeqNum      txSeqNum
	shardServers  map[*shardsServer]struct{}
	isValid       bool
	invalidatedBy *shardsServer
}
