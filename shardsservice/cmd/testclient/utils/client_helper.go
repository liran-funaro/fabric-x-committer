package utils

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

var logger = logging.New("shardsservice-client")

type ClientInputConfig struct {
	BlockCount     int
	BlockSize      int
	TxSize         int
	SignatureBytes bool
	InputDelay     test.Distribution
}

type ClientConfig struct {
	Connections        []*connection.DialConfig
	NumShardsPerServer int
	Input              ClientInputConfig
}

type ShardClient struct {
	blockGenerator  *testutil.BlockGenerator
	delayGenerator  *test.DelayGenerator
	tracker         *test.AsyncRequestTracker
	phaseOneStreams []shardsservice.Shards_StartPhaseOneStreamClient
	phaseTwoStreams []shardsservice.Shards_StartPhaseTwoStreamClient
	maxBlockCount   uint64
	once            sync.Once
	started         sync.WaitGroup
}

func NewClient(clientConfig ClientConfig) (*ShardClient, error) {
	phaseOneStreams := make([]shardsservice.Shards_StartPhaseOneStreamClient, len(clientConfig.Connections))
	phaseTwoStreams := make([]shardsservice.Shards_StartPhaseTwoStreamClient, len(clientConfig.Connections))
	var err error
	for i, conn := range clientConfig.Connections {
		phaseOneStreams[i], phaseTwoStreams[i], err = startStreams(conn, i*clientConfig.NumShardsPerServer+1, (i+1)*clientConfig.NumShardsPerServer)
		if err != nil {
			return nil, err
		}
	}
	return &ShardClient{
		blockGenerator:  testutil.NewBlockGenerator(clientConfig.Input.BlockSize, clientConfig.Input.TxSize, clientConfig.Input.SignatureBytes),
		delayGenerator:  test.NewDelayGenerator(clientConfig.Input.InputDelay, 10),
		tracker:         test.NewAsyncTracker(),
		phaseOneStreams: phaseOneStreams,
		phaseTwoStreams: phaseTwoStreams,
		maxBlockCount:   uint64(clientConfig.Input.BlockCount),
	}, nil
}

func (c *ShardClient) CleanUp() {
	for _, s := range c.phaseOneStreams {
		s.CloseSend()
	}
	for _, s := range c.phaseTwoStreams {
		s.CloseSend()
	}
	c.blockGenerator.Stop()
}

func (c *ShardClient) Start() {
	c.started.Add(1)
	outputReceived := c.tracker.Start()
	var totalBlocks uint64

	for i := 0; i < len(c.phaseOneStreams); i++ {
		go c.handlePhaseOne(c.phaseOneStreams[i], &totalBlocks)
		go c.handlePhaseTwo(c.phaseOneStreams[i], c.phaseTwoStreams[i], outputReceived)
	}
}

func (c *ShardClient) Debug() {
	fmt.Printf("Sent transactions with rate: %d TPS.\n", c.tracker.RequestsPer(time.Second))
}

func (c *ShardClient) WaitUntilDone() test.AsyncTrackerStats {
	c.started.Wait()
	return c.tracker.WaitUntilDone()
}

func (c *ShardClient) handlePhaseTwo(phaseOneStream shardsservice.Shards_StartPhaseOneStreamClient, phaseTwoStream shardsservice.Shards_StartPhaseTwoStreamClient, outputReceived func(int)) {
	for {
		p1Response, err := phaseOneStream.Recv()
		if err != nil {
			panic(err)
		}
		p2Request, err := createPhaseTwoRequestBatch(p1Response)
		if err != nil {
			panic(err)
		}
		if err := phaseTwoStream.Send(p2Request); err != nil {
			panic(err)
		}
		outputReceived(len(p2Request.Requests))
	}
}

func (c *ShardClient) handlePhaseOne(phaseOneStream shardsservice.Shards_StartPhaseOneStreamClient, totalBlocks *uint64) {
	for {
		block := <-c.blockGenerator.OutputChan()
		c.tracker.SubmitRequests(len(block.Txs))
		c.once.Do(func() {
			c.started.Done()
		})
		atomic.AddUint64(totalBlocks, 1)

		utils.Must(phaseOneStream.Send(createPhaseOneRequestBatch(block)))
		if c.maxBlockCount >= 0 && *totalBlocks >= c.maxBlockCount-1 {
			fmt.Printf("Sent %d blocks. Finishing.\n", *totalBlocks)
			return
		}
		c.delayGenerator.Next()
	}

}

func createPhaseTwoRequestBatch(responseBatch *shardsservice.PhaseOneResponseBatch) (*shardsservice.PhaseTwoRequestBatch, error) {
	requests := make([]*shardsservice.PhaseTwoRequest, len(responseBatch.Responses))

	for i, response := range responseBatch.Responses {
		if response.Status == shardsservice.PhaseOneResponse_CANNOT_COMMITTED {
			return nil, errors.New("cannot commit")
		}
		requests[i] = &shardsservice.PhaseTwoRequest{
			BlockNum:    response.BlockNum,
			TxNum:       response.TxNum,
			Instruction: shardsservice.PhaseTwoRequest_COMMIT,
		}
	}
	return &shardsservice.PhaseTwoRequestBatch{Requests: requests}, nil
}

func createPhaseOneRequestBatch(b *token.Block) *shardsservice.PhaseOneRequestBatch {
	requests := make([]*shardsservice.PhaseOneRequest, len(b.Txs))
	for i, tx := range b.Txs {
		requests[i] = &shardsservice.PhaseOneRequest{
			BlockNum: b.Number,
			TxNum:    uint64(i),
			ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
				1: {SerialNumbers: tx.SerialNumbers},
			},
		}
	}
	return &shardsservice.PhaseOneRequestBatch{Requests: requests}
}

func startStreams(conn *connection.DialConfig, firstShardId, lastShardId int) (shardsservice.Shards_StartPhaseOneStreamClient, shardsservice.Shards_StartPhaseTwoStreamClient, error) {
	clientConnection, _ := connection.Connect(conn)
	client := shardsservice.NewShardsClient(clientConnection)

	_, err := client.DeleteShards(context.Background(), &shardsservice.Empty{})
	if err != nil {
		return nil, nil, err
	}
	logger.Infoln("Deleted previous shards")

	_, err = client.SetupShards(context.Background(), &shardsservice.ShardsSetupRequest{FirstShardId: uint32(firstShardId), LastShardId: uint32(lastShardId)})
	if err != nil {
		return nil, nil, err
	}
	logger.Infof("Set up shard service for shard range %d - %d", firstShardId, lastShardId)

	phaseOneStream, err := client.StartPhaseOneStream(context.Background())
	if err != nil {
		return nil, nil, err
	}

	phaseTwoStream, err := client.StartPhaseTwoStream(context.Background())
	if err != nil {
		return phaseOneStream, nil, err
	}

	return phaseOneStream, phaseTwoStream, nil
}
