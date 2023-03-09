package shardsservice

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
)

func TestExecutePhaseOneAndTwoWithMultiShards(t *testing.T) {
	phaseOneResponses := make(chan []*PhaseOneResponse, 10)

	c := ReadConfig()
	//t.Run("simple phase one execution", func(t *testing.T) {
	dbConfig := &DatabaseConfig{Type: GoLevelDb, RootDir: "./"}
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	si, err := newShardInstances(phaseOneResponses, dbConfig, c.Limits, m)
	require.NoError(t, err)
	defer si.deleteAll()

	go si.accumulatedPhaseOneResponses(2, 50*time.Millisecond)

	//setup 4 shards
	shardIDs := []uint32{1, 2, 3, 4, 5, 6}
	for _, id := range shardIDs {
		require.NoError(t, si.setup(id, c.Limits))
	}

	p1 := &PhaseOneRequestBatch{
		Requests: []*PhaseOneRequest{
			{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*SerialNumbers{
					1: {
						SerialNumbers: [][]byte{
							[]byte("key1"),
							[]byte("key2"),
							[]byte("key3"),
						},
					},
					2: {
						SerialNumbers: [][]byte{
							[]byte("key4"),
							[]byte("key5"),
						},
					},
				},
			},
		},
	}

	expectedP1Response := []*PhaseOneResponse{
		{
			BlockNum: 1,
			TxNum:    1,
			Status:   PhaseOneResponse_CAN_COMMIT,
		},
	}

	si.executePhaseOne(p1)
	p1Responses := <-phaseOneResponses
	require.ElementsMatch(t, expectedP1Response, p1Responses)

	p2 := &PhaseTwoRequestBatch{
		Requests: []*PhaseTwoRequest{
			{
				BlockNum:    1,
				TxNum:       1,
				Instruction: PhaseTwoRequest_COMMIT,
			},
		},
	}
	si.executePhaseTwo(p2)
	time.Sleep(2 * time.Second)

	p1 = &PhaseOneRequestBatch{
		Requests: []*PhaseOneRequest{
			{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*SerialNumbers{
					1: {
						SerialNumbers: [][]byte{
							[]byte("key1"),
							[]byte("key2"),
						},
					},
					2: {
						SerialNumbers: [][]byte{
							[]byte("key3"),
							[]byte("key4"),
						},
					},
					3: {
						SerialNumbers: [][]byte{
							[]byte("key5"),
						},
					},
					4: {
						SerialNumbers: [][]byte{
							[]byte("key7"),
						},
					},
					5: {
						SerialNumbers: [][]byte{
							[]byte("key8"),
						},
					},
					6: {
						SerialNumbers: [][]byte{
							[]byte("key9"),
						},
					},
				},
			},
		},
	}

	expectedP1Response = []*PhaseOneResponse{
		{
			BlockNum: 1,
			TxNum:    1,
			Status:   PhaseOneResponse_CANNOT_COMMITTED,
		},
	}

	si.executePhaseOne(p1)
	p1Responses = <-phaseOneResponses
	require.ElementsMatch(t, expectedP1Response, p1Responses)
	//})
}
