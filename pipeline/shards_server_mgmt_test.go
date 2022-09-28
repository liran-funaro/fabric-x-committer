package pipeline

import (
	"encoding/binary"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/protobuf/proto"
)

func TestShardsServerMgr(t *testing.T) {
	config := &ShardsServerMgrConfig{
		Servers: []*ShardServerInstanceConfig{
			{connection.CreateEndpoint("localhost:6000"), 1}, // shard 0
			{connection.CreateEndpoint("localhost:6001"), 3}, // shard 1, shard 2, and shard 3
			{connection.CreateEndpoint("localhost:6002"), 5}, // shards 4,5,6,7, and 8
		},
		DeleteExistingShards: true,
	}

	shardsServers, err := testutil.StartsShardsGrpcServers(
		testutil.DefaultPhaseOneBehavior,
		config.GetEndpoints(),
	)
	require.NoError(t, err)
	defer func() {
		for _, s := range shardsServers {
			s.Stop()
		}
	}()

	testServerImpl0 := &testServerImpl{}
	testServerImpl1 := &testServerImpl{}
	testServerImpl2 := &testServerImpl{}
	shardsServers[0].ShardsServerImpl.PhaseOneBehavior = testServerImpl0.PhaseOneBehavior
	shardsServers[1].ShardsServerImpl.PhaseOneBehavior = testServerImpl1.PhaseOneBehavior
	shardsServers[2].ShardsServerImpl.PhaseOneBehavior = testServerImpl2.PhaseOneBehavior

	m, err := newShardsServerMgr(config)
	require.NoError(t, err)
	defer m.stop()

	m.inputChan <- map[TxSeqNum][][]byte{
		{BlkNum: 1, TxNum: 1}: {getForTestBytesStartingWith(0), getForTestBytesStartingWith(1)}, // should split across server0 and server1
		{BlkNum: 1, TxNum: 2}: {getForTestBytesStartingWith(2), getForTestBytesStartingWith(3)}, // should go to server1 only
		{BlkNum: 1, TxNum: 3}: {getForTestBytesStartingWith(3), getForTestBytesStartingWith(8)}, // should split across server1 and server2
	}

	status := []*TxStatus{}
	for len(status) < 3 {
		status = append(status, <-m.outputChan...)
	}
	require.ElementsMatch(t,
		status,
		[]*TxStatus{
			{
				TxSeqNum: TxSeqNum{
					BlkNum: 1,
					TxNum:  1,
				},
				IsValid: true,
			},
			{
				TxSeqNum: TxSeqNum{
					BlkNum: 1,
					TxNum:  2,
				},
				IsValid: true,
			},
			{
				TxSeqNum: TxSeqNum{
					BlkNum: 1,
					TxNum:  3,
				},
				IsValid: true,
			},
		},
	)

	require.Len(t, testServerImpl0.batches, 1)
	require.Len(t, testServerImpl0.batches[0].Requests, 1)
	require.True(t,
		proto.Equal(
			testServerImpl0.batches[0].Requests[0],
			&shardsservice.PhaseOneRequest{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
					0: {
						SerialNumbers: [][]byte{getForTestBytesStartingWith(0)},
					},
				},
				IsCompleteTx: false,
			},
		),
	)

	require.Len(t, testServerImpl1.batches, 1)
	require.Len(t, testServerImpl1.batches[0].Requests, 3)
	testSortProtoPhaseOneRequests(testServerImpl1.batches[0].Requests)
	require.True(
		t,
		proto.Equal(
			testServerImpl1.batches[0],
			&shardsservice.PhaseOneRequestBatch{
				Requests: []*shardsservice.PhaseOneRequest{
					{
						BlockNum: 1,
						TxNum:    1,
						ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
							1: {
								SerialNumbers: [][]byte{getForTestBytesStartingWith(1)},
							},
						},
						IsCompleteTx: false,
					},
					{
						BlockNum: 1,
						TxNum:    2,
						ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
							2: {
								SerialNumbers: [][]byte{getForTestBytesStartingWith(2)},
							},
							3: {
								SerialNumbers: [][]byte{getForTestBytesStartingWith(3)},
							},
						},
						IsCompleteTx: true,
					},
					{
						BlockNum: 1,
						TxNum:    3,
						ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
							3: {
								SerialNumbers: [][]byte{getForTestBytesStartingWith(3)},
							},
						},
						IsCompleteTx: false,
					},
				},
			},
		),
	)

	require.Len(t, testServerImpl2.batches, 1)
	require.Len(t, testServerImpl2.batches[0].Requests, 1)
	require.True(t,
		proto.Equal(
			testServerImpl2.batches[0].Requests[0],
			&shardsservice.PhaseOneRequest{
				BlockNum: 1,
				TxNum:    3,
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
					8: {
						SerialNumbers: [][]byte{getForTestBytesStartingWith(8)},
					},
				},
				IsCompleteTx: false,
			},
		),
	)
}

func TestComputeShardId(t *testing.T) {
	m := &shardsServerMgr{
		numShards: 16,
	}
	for i := uint16(0); i <= 500; i++ {
		b := getForTestBytesStartingWith(i)
		expectedShardId := int(i % 16)
		require.Equal(t, expectedShardId, m.computeShardId(b))
	}
}

type testServerImpl struct {
	batches []*shardsservice.PhaseOneRequestBatch
}

func (i *testServerImpl) PhaseOneBehavior(requestBatch *shardsservice.PhaseOneRequestBatch) *shardsservice.PhaseOneResponseBatch {
	i.batches = append(i.batches, requestBatch)
	return testutil.DefaultPhaseOneBehavior(requestBatch)
}

func getForTestBytesStartingWith(i uint16) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, i)
	return append(b, []byte("randomBytes")...)
}

func testSortProtoPhaseOneRequests(r []*shardsservice.PhaseOneRequest) {
	sort.Slice(r,
		func(i, j int) bool {
			if r[i].BlockNum < r[j].BlockNum {
				return true
			}
			return r[i].TxNum < r[j].TxNum
		},
	)
}
