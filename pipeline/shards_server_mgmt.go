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

type shardsServerMgr struct {
	phaseOneStreams map[int]shardsservice.Server_StartPhaseOneStreamClient
	phaseTwoStreams map[int]shardsservice.Server_StartPhaseTwoStreamClient
	numShards       uint16
}

type shardsServerConfig struct {
	ShardsServersToNumShards map[string]int
}

func newShardsServerMgr(c *shardsServerConfig) (*shardsServerMgr, error) {
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

func (m *shardsServerMgr) computeShardId(key []byte) int {
	return int(binary.BigEndian.Uint16(key[0:2]) % m.numShards)
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
