package pipeline

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"google.golang.org/grpc"
)

type shardsServerMgrConfig struct {
	ShardsServersToNumShards map[string]int
}

type shardsServerMgr struct {
	phaseOneStreams map[int]shardsservice.Server_StartPhaseOneStreamClient
	phaseTwoStreams map[int]shardsservice.Server_StartPhaseTwoStreamClient

	shardIdToServer map[int]*shardServer
	numShards       uint16
}

type shardServer struct {
	phaseOneStream shardsservice.Server_StartPhaseOneStreamClient
	phaseTwoStream shardsservice.Server_StartPhaseTwoStreamClient
}

func newShardsServerMgr(c *shardsServerMgrConfig) (*shardsServerMgr, error) {
	phaseOneStreams := map[int]shardsservice.Server_StartPhaseOneStreamClient{}
	phaseTwoStreams := map[int]shardsservice.Server_StartPhaseTwoStreamClient{}

	serverAddresses := []string{}
	for a := range c.ShardsServersToNumShards {
		serverAddresses = append(serverAddresses, a)
	}
	sort.Strings(serverAddresses)

	firstShardNum := 0
	for _, a := range serverAddresses {
		lastShardNum := firstShardNum + c.ShardsServersToNumShards[a] - 1
		phaseOneStream, phaseTwoStream, err := initShardsServerStreams(a, firstShardNum, lastShardNum)
		if err != nil {
			return nil, err
		}
		for i := firstShardNum; i <= lastShardNum; i++ {
			phaseOneStreams[i] = phaseOneStream
			phaseTwoStreams[i] = phaseTwoStream
		}
		firstShardNum = lastShardNum + 1
	}

	return &shardsServerMgr{
		phaseOneStreams: phaseOneStreams,
		phaseTwoStreams: phaseTwoStreams,
		numShards:       uint16(firstShardNum - 1),
	}, nil
}

type phaseOneReq struct {
	m map[txSeqNum]*shardsservice.PhaseOneRequest
}

func (m *shardsServerMgr) processTxs(txs map[txSeqNum][][]byte) {
	phaseOneReqs := map[*shardServer]*phaseOneReq{}
	for txSeq, sns := range txs {
		for _, sn := range sns {
			shardID := m.computeShardId(sn)
			shardServer := m.shardIdToServer[shardID]

			reqs, ok := phaseOneReqs[shardServer]
			if !ok {
				reqs = &phaseOneReq{
					m: map[txSeqNum]*shardsservice.PhaseOneRequest{},
				}
				phaseOneReqs[shardServer] = reqs
			}

			req, ok := reqs.m[txSeq]
			if !ok {
				req = &shardsservice.PhaseOneRequest{
					BlockNum:               txSeq.blkNum,
					TxNum:                  txSeq.txNum,
					ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{},
				}
				reqs.m[txSeq] = req
			}

			serialNums, ok := req.ShardidToSerialNumbers[uint32(shardID)]
			if !ok {
				serialNums = &shardsservice.SerialNumbers{}
				req.ShardidToSerialNumbers[uint32(shardID)] = serialNums
			}
			serialNums.SerialNumbers = append(serialNums.SerialNumbers, sn)
		}
	}

}

func (m *shardsServerMgr) computeShardId(sn []byte) int {
	return int(binary.BigEndian.Uint16(sn[0:2]) % m.numShards)
}

func initShardsServerStreams(serverAddr string, firstShardNum, lastShardNum int) (
	shardsservice.Server_StartPhaseOneStreamClient,
	shardsservice.Server_StartPhaseTwoStreamClient,
	error,
) {
	conn, err := grpc.Dial(fmt.Sprintf("%s:%d", serverAddr, config.GRPC_PORT), grpc.WithInsecure())
	if err != nil {
		return nil, nil, err
	}
	client := shardsservice.NewServerClient(conn)

	if _, err := client.DeleteShards(context.Background(), &shardsservice.Empty{}); err != nil {
		return nil, nil, err
	}

	if _, err := client.SetupShards(context.Background(),
		&shardsservice.ShardsSetupRequest{
			FirstShardNum: uint32(firstShardNum),
			LastShardNum:  uint32(lastShardNum),
		},
	); err != nil {
		return nil, nil, err
	}

	phaseOneStream, err := client.StartPhaseOneStream(context.Background())
	if err != nil {
		return nil, nil, err
	}

	phaseTwoStream, err := client.StartPhaseTwoStream(context.Background())
	if err != nil {
		return nil, nil, err
	}

	return phaseOneStream, phaseTwoStream, err
}
