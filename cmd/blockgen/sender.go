package main

import (
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
)

func sendTransactions(
	blockGen *loadgen.BlockStreamGenerator,
	stream any,
	rateLimit int,
	latencySamplingInterval time.Duration,
) error {
	var send func(blk *protoblocktx.Block) error

	switch s := stream.(type) {
	case protocoordinatorservice.Coordinator_BlockProcessingClient:
		send = func(blk *protoblocktx.Block) error {
			return s.Send(blk)
		}
	case protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient:
		send = func(blk *protoblocktx.Block) error {
			txBatch := &protovcservice.TransactionBatch{}
			for _, tx := range blk.Txs {
				txBatch.Transactions = append(
					txBatch.Transactions,
					&protovcservice.Transaction{
						ID:         tx.Id,
						Namespaces: tx.Namespaces,
					},
				)
			}
			return s.Send(txBatch)
		}
	}

	samplingTicker := time.NewTicker(latencySamplingInterval)

	numBlocksPerSec := rateLimit / blockSize
	blocksSent := 0
	start := time.Now()
	for {
		select {
		case <-stopSender:
			return nil
		default:
		}

		blk := <-blockGen.BlockQueue
		if err := send(blk); err != nil {
			return err
		}

		metrics.addToCounter(metrics.blockSentTotal, 1)
		metrics.addToCounter(metrics.transactionSentTotal, len(blk.Txs))

		sampleTxsForLatencyMeasurement(samplingTicker, blk)

		blocksSent++
		waited := waitIfNeeded(start, blocksSent, numBlocksPerSec)
		if waited {
			start = time.Now()
		}
	}
}

func sampleTxsForLatencyMeasurement(t *time.Ticker, blk *protoblocktx.Block) {
	select {
	case <-t.C:
		t := time.Now()
		for _, tx := range blk.Txs {
			latencyTracker.Store(tx.Id, t)
		}
	default:
	}
}

func waitIfNeeded(start time.Time, blocksSent, numBlocksPerSec int) (waited bool) {
	if blocksSent%numBlocksPerSec != 0 {
		return false
	}

	elapsed := time.Since(start)
	if elapsed < time.Second {
		time.Sleep(time.Second - elapsed)
	}

	return true
}
