package shardsservice

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/protos/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/db"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/db/goleveldb"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

func init() {
	db.Register(goleveldb.GoLevelDb, func(path string) (db.Database, error) {
		return goleveldb.Open(path)
	})
}

var TestConfig = ShardServiceConfig{
	Server: &connection.ServerConfig{Endpoint: connection.Endpoint{
		Host: "localhost",
		Port: 5101,
	}},
	Database: &DatabaseConfig{
		Type:    "goleveldb",
		RootDir: "./",
	},
	Limits: &LimitsConfig{
		MaxPhaseOneResponseBatchItemCount: 100,
		PhaseOneResponseCutTimeout:        10 * time.Millisecond,
		MaxPhaseOneProcessingWorkers:      50,
		MaxPhaseTwoProcessingWorkers:      50,
		MaxPendingCommitsBufferSize:       100,
		MaxShardInstancesBufferSize:       100,
	},
}

func TestExecutePhaseOneAndTwoWithMultiShards(t *testing.T) {
	phaseOneResponses := make(chan []*shardsservice.PhaseOneResponse, 10)

	c := TestConfig
	m := &metrics.Metrics{Enabled: false}

	//t.Run("simple phase one execution", func(t *testing.T) {
	dbConfig := &DatabaseConfig{Type: goleveldb.GoLevelDb, RootDir: "./"}
	si, err := newShardInstances(phaseOneResponses, dbConfig, c.Limits, m)
	require.NoError(t, err)
	defer si.deleteAll()

	go si.accumulatedPhaseOneResponses(2, 50*time.Millisecond)

	//setup 4 shards
	shardIDs := []uint32{1, 2, 3, 4, 5, 6}
	for _, id := range shardIDs {
		require.NoError(t, si.setup(id, c.Limits))
	}

	p1 := &shardsservice.PhaseOneRequestBatch{
		Requests: []*shardsservice.PhaseOneRequest{
			{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
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

	expectedP1Response := []*shardsservice.PhaseOneResponse{
		{
			BlockNum: 1,
			TxNum:    1,
			Status:   shardsservice.PhaseOneResponse_CAN_COMMIT,
		},
	}

	si.executePhaseOne(p1)
	p1Responses := <-phaseOneResponses
	require.ElementsMatch(t, expectedP1Response, p1Responses)

	p2 := &shardsservice.PhaseTwoRequestBatch{
		Requests: []*shardsservice.PhaseTwoRequest{
			{
				BlockNum:    1,
				TxNum:       1,
				Instruction: shardsservice.PhaseTwoRequest_COMMIT,
			},
		},
	}
	si.executePhaseTwo(p2)
	time.Sleep(2 * time.Second)

	p1 = &shardsservice.PhaseOneRequestBatch{
		Requests: []*shardsservice.PhaseOneRequest{
			{
				BlockNum: 1,
				TxNum:    1,
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
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

	expectedP1Response = []*shardsservice.PhaseOneResponse{
		{
			BlockNum: 1,
			TxNum:    1,
			Status:   shardsservice.PhaseOneResponse_CANNOT_COMMITTED,
		},
	}

	si.executePhaseOne(p1)
	p1Responses = <-phaseOneResponses
	require.ElementsMatch(t, expectedP1Response, p1Responses)
	//})
}
