package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/shardsservice/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type loadConfig struct {
	numTxPerBlock  int
	serialNumPerTx int
	numBlocks      int
	blockRate      int
}

var lc = &loadConfig{
	numTxPerBlock:  100,
	serialNumPerTx: 1,
	numBlocks:      100000,
	blockRate:      1000,
}

func main() {
	c := shardsservice.ReadConfig()

	go connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(server *grpc.Server) {
		shardsservice.RegisterShardsServer(server, shardsservice.NewShardsCoordinator(c.Database, c.Limits, metrics.New(c.Prometheus.Enabled)))
	})

	bg := testutil.NewBlockGenerator(lc.numTxPerBlock, lc.serialNumPerTx, false)
	defer bg.Stop()

	processBlocks(c.Endpoint.Address(), bg, lc)
}

func processBlocks(serverAddr string, bg *testutil.BlockGenerator, lc *loadConfig) {
	cliOpts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	clientConn, err := grpc.Dial(serverAddr, cliOpts...)
	if err != nil {
		panic(err)
	}

	client := shardsservice.NewShardsClient(clientConn)
	_, err = client.DeleteShards(context.Background(), &shardsservice.Empty{})
	if err != nil {
		panic(err)
	}

	_, err = client.DeleteShards(context.Background(), &shardsservice.Empty{})
	if err != nil {
		panic(err)
	}

	_, err = client.SetupShards(context.Background(), &shardsservice.ShardsSetupRequest{FirstShardId: 1, LastShardId: 1})
	if err != nil {
		panic(err)
	}

	phaseOneStream, err := client.StartPhaseOneStream(context.Background())
	if err != nil {
		panic(err)
	}
	defer phaseOneStream.CloseSend()

	lastBlock := make(chan *token.Block)

	go executePhaseOne(bg, lc, phaseOneStream, lastBlock)

	phaseTwoStream, err := client.StartPhaseTwoStream(context.Background())
	if err != nil {
		panic(err)
	}
	defer phaseTwoStream.CloseSend()

	wg := &sync.WaitGroup{}
	wg.Add(1)
	go executePhaseTwo(phaseOneStream, phaseTwoStream, wg, lc, lastBlock)
	wg.Wait()
}

func executePhaseOne(bg *testutil.BlockGenerator, lc *loadConfig, phaseOneStream shardsservice.Shards_StartPhaseOneStreamClient, lastBlock chan *token.Block) {
	var blockSendInterval int
	if lc.blockRate != -1 {
		blockSendInterval = 1000000 / lc.blockRate
	}

	for i := 0; i < lc.numBlocks; i++ {
		start := time.Now()
		b := <-bg.OutputChan()

		phaseOneRequests := []*shardsservice.PhaseOneRequest{}

		for txNum, tx := range b.Txs {
			phaseOneRequests = append(
				phaseOneRequests,
				&shardsservice.PhaseOneRequest{
					BlockNum: b.Number,
					TxNum:    uint64(txNum),
					ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
						1: {SerialNumbers: tx.SerialNumbers},
					},
				},
			)
		}

		if err := phaseOneStream.Send(&shardsservice.PhaseOneRequestBatch{
			Requests: phaseOneRequests,
		}); err != nil {
			panic(err)
		}
		end := time.Since(start)

		if lc.blockRate != -1 {
			time.Sleep(time.Duration(blockSendInterval)*time.Microsecond - end)
		}

		if i == lc.numBlocks-1 {
			lastBlock <- b
		}
	}

	fmt.Println("phase one execution is done")
}

func executePhaseTwo(phaseOneStream shardsservice.Shards_StartPhaseOneStreamClient, phaseTwoStream shardsservice.Shards_StartPhaseTwoStreamClient, wg *sync.WaitGroup, lc *loadConfig, lastBlock chan *token.Block) {
	var txProcessed uint32
	var totalTxProcessed uint32

	tick := time.NewTicker(1 * time.Second)
	go func() {
		for {
			select {
			case <-tick.C:
				txProcessed := atomic.SwapUint32(&txProcessed, 0)
				log.Printf("received phase one response and sent phase two requests for %d transactions", txProcessed)
			}
		}
	}()

	start := time.Now()
	for {
		phase1Responses, err := phaseOneStream.Recv()
		if err != nil {
			panic(err)
		}

		numTxs := uint32(len(phase1Responses.Responses))
		atomic.AddUint32(&txProcessed, numTxs)
		totalTxProcessed += numTxs

		phase2Requests := []*shardsservice.PhaseTwoRequest{}

		for _, resp := range phase1Responses.Responses {
			if resp.Status == shardsservice.PhaseOneResponse_CANNOT_COMMITTED {
				panic(err)
			}
			phase2Requests = append(phase2Requests, &shardsservice.PhaseTwoRequest{
				BlockNum:    resp.BlockNum,
				TxNum:       resp.TxNum,
				Instruction: shardsservice.PhaseTwoRequest_COMMIT,
			})
		}

		if err := phaseTwoStream.Send(&shardsservice.PhaseTwoRequestBatch{
			Requests: phase2Requests,
		}); err != nil {
			panic(err)
		}

		if totalTxProcessed == uint32(lc.numBlocks)*uint32(lc.numTxPerBlock) {
			elapsed := time.Since(start)
			log.Printf("processed %d transactions in %s", totalTxProcessed, elapsed)

			elapsedSeconds := float64(elapsed) / float64(time.Second)
			log.Printf("average phase1/phase2 tps is %f", float64(totalTxProcessed)/float64(elapsedSeconds))
			break
		}
	}

	// wait till the commit of last transaction
	b := <-lastBlock
	lastTx := b.Txs[len(b.Txs)-1]
	if err := phaseOneStream.Send(&shardsservice.PhaseOneRequestBatch{
		Requests: []*shardsservice.PhaseOneRequest{
			{
				BlockNum: b.Number,
				TxNum:    uint64(len(b.Txs) - 1),
				ShardidToSerialNumbers: map[uint32]*shardsservice.SerialNumbers{
					1: {SerialNumbers: lastTx.SerialNumbers},
				},
			},
		},
	}); err != nil {
		panic(err)
	}

	phase1Responses, err := phaseOneStream.Recv()
	if err != nil {
		panic(err)
	}

	for _, resp := range phase1Responses.Responses {
		if resp.Status == shardsservice.PhaseOneResponse_CANNOT_COMMITTED {
			elapsed := time.Since(start)
			log.Printf("all commit done in %s seconds", elapsed)

			elapsedSeconds := float64(elapsed) / float64(time.Second)
			log.Printf("average commit tps is %f", float64(totalTxProcessed)/float64(elapsedSeconds))
			wg.Done()
		}

	}
}
