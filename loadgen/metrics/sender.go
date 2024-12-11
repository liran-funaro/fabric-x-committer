package metrics

import (
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

type storer interface {
	Store(key, value any)
}

type latencySenderTracker struct {
	latencyTracker storer
	batchSampler   batchTracingSampler
	blockSampler   blockTracingSampler
	txSampler      txTracingSampler
}

func (c *latencySenderTracker) OnSendBlock(block *protoblocktx.Block) {
	logger.Debugf("Sent block [%d:%d]", block.Number, len(block.Txs))
	if !c.blockSampler(block.Number) {
		return
	}
	logger.Infof("Block [%d:%d] is tracked.", block.Number, len(block.Txs))
	t := time.Now()
	for _, tx := range block.Txs {
		c.latencyTracker.Store(tx.Id, t)
	}
}

func (c *latencySenderTracker) OnSendTransaction(txId string) {
	if c.txSampler(txId) {
		c.latencyTracker.Store(txId, time.Now())
	}
}
