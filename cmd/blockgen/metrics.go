package main

import (
	"sync"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type perfMetrics struct {
	provider                        metrics.Provider
	blockSentTotal                  prometheus.Counter
	transactionSentTotal            prometheus.Counter
	transactionReceivedTotal        prometheus.Counter
	validTransactionLatencySecond   prometheus.Histogram
	invalidTransactionLatencySecond prometheus.Histogram
}

func newBlockgenServiceMetrics(p metrics.Provider) *perfMetrics {
	buckets := p.Buckets()
	return &perfMetrics{
		provider: p,
		blockSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "block_sent_total",
			Help:      "Total number of blocks sent by the block generator",
		}),
		transactionSentTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_sent_total",
			Help:      "Total number of transactions sent by the block generator",
		}),
		transactionReceivedTotal: p.NewCounter(prometheus.CounterOpts{
			Namespace: "blockgen",
			Name:      "transaction_received_total",
			Help:      "Total number of transactions received by the block generator",
		}),
		validTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "blockgen",
			Subsystem: "",
			Name:      "valid_transaction_latency_seconds",
			Help:      "Latency of transactions in seconds",
			Buckets:   buckets,
		}),
		invalidTransactionLatencySecond: p.NewHistogram(prometheus.HistogramOpts{
			Namespace: "blockgen",
			Subsystem: "",
			Name:      "invalid_transaction_latency_seconds",
			Help:      "Latency of invalid transactions in seconds",
			Buckets:   buckets,
		}),
	}
}

type ClientTracker struct {
	*senderTracker
	*receiverTracker
}

func NewClientTracker(logger CmdLogger, metrics *perfMetrics, samplerConfig metrics.SamplerProvider) *ClientTracker {
	latencyTracker := &sync.Map{}
	return &ClientTracker{
		senderTracker: &senderTracker{
			logger:         logger,
			metrics:        metrics,
			latencyTracker: latencyTracker,
			batchSampler:   samplerConfig.BatchSampler(),
			blockSampler:   samplerConfig.BlockSampler(),
			txSampler:      samplerConfig.TxSampler(),
		},
		receiverTracker: &receiverTracker{
			logger:         logger,
			metrics:        metrics,
			latencyTracker: latencyTracker,
		},
	}
}

type receiverTracker struct {
	logger         CmdLogger
	metrics        *perfMetrics
	latencyTracker *sync.Map
}

func (c *receiverTracker) OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch) {
	logger.Debugf("Received coordinator batch with %d items", len(batch.TxsValidationStatus))
	for _, tx := range batch.TxsValidationStatus {
		c.OnReceiveTransaction(tx.TxId, tx.Status == protoblocktx.Status_COMMITTED)
	}
}

func (c *receiverTracker) OnReceiveVCBatch(batch *protovcservice.TransactionStatus) {
	logger.Debugf("Received VC batch with %d items", len(batch.Status))
	for id, status := range batch.Status {
		c.OnReceiveTransaction(id, status == protoblocktx.Status_COMMITTED)
	}
}

func (c *receiverTracker) OnReceiveSVBatch(batch *sigverification.ResponseBatch) {
	logger.Debugf("Received SV batch with %d items", len(batch.Responses))
	for _, response := range batch.Responses {
		c.OnReceiveTransaction(response.TxId, response.IsValid)
	}
}

func (c *receiverTracker) OnReceiveTransaction(txID string, success bool) {
	c.metrics.transactionReceivedTotal.Add(float64(1))
	if t, ok := c.latencyTracker.LoadAndDelete(txID); ok {
		start, _ := t.(time.Time)
		elapsed := time.Since(start)

		if success {
			c.metrics.validTransactionLatencySecond.Observe(elapsed.Seconds())
		} else {
			c.metrics.invalidTransactionLatencySecond.Observe(elapsed.Seconds())
		}
	}
}

func (c *receiverTracker) OnReceiveBlock(block *common.Block) {
	logger.Debugf("Received block [%d:%d]", block.Header.Number, len(block.Data.Data))
	statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	for i, data := range block.Data.Data {
		if _, channelHeader, err := serialization.UnwrapEnvelope(data); err == nil {
			c.OnReceiveTransaction(channelHeader.TxId, aggregator.StatusInverseMap[statusCodes[i]] == protoblocktx.Status_COMMITTED)
		}
	}
}

type senderTracker struct {
	logger         CmdLogger
	metrics        *perfMetrics
	latencyTracker *sync.Map
	batchSampler   latency.BatchTracingSampler
	blockSampler   latency.BlockTracingSampler
	txSampler      latency.TxTracingSampler
}

func (c *senderTracker) OnSendSVBatch(batch *sigverification.RequestBatch) {
	logger.Debugf("Sent SV batch with %d items", len(batch.Requests))
	c.metrics.blockSentTotal.Add(float64(1))
	c.metrics.transactionSentTotal.Add(float64(len(batch.Requests)))
	if c.batchSampler() {
		t := time.Now()
		for _, req := range batch.Requests {
			c.latencyTracker.Store(req.Tx.Id, t)
		}
	}
}

func (c *senderTracker) OnSendBlock(block *protoblocktx.Block) {
	logger.Debugf("Sent block [%d:%d]", block.Number, len(block.Txs))
	c.metrics.blockSentTotal.Add(float64(1))
	c.metrics.transactionSentTotal.Add(float64(len(block.Txs)))
	if c.blockSampler(block.Number) {
		t := time.Now()
		for _, tx := range block.Txs {
			c.latencyTracker.Store(tx.Id, t)
		}
	}
}

func (c *senderTracker) OnSendTransaction(txId string) {
	logger.Debugf("Sent TX [%s]", txId)
	c.metrics.transactionSentTotal.Add(float64(1))

	if c.txSampler(txId) {
		c.latencyTracker.Store(txId, time.Now())
	}
}
