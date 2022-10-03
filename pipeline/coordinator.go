package pipeline

import (
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

type Coordinator struct {
	dependencyMgr   *dependencyMgr
	sigVerifierMgr  *sigVerifierMgr
	shardsServerMgr *shardsServerMgr
	outputChan      chan []*TxStatus
	stopSignalCh    chan struct{}
	metrics         *metrics.Metrics
}

func NewCoordinator(sigVerifierMgrConfig *SigVerifierMgrConfig, shardsServerMgrConfig *ShardsServerMgrConfig, metrics *metrics.Metrics) (*Coordinator, error) {
	sigVerifierMgr, err := newSigVerificationMgr(sigVerifierMgrConfig, metrics)
	if err != nil {
		return nil, err
	}
	shardsServerMgr, err := newShardsServerMgr(shardsServerMgrConfig, metrics)
	if err != nil {
		return nil, err
	}
	c := &Coordinator{
		dependencyMgr:   newDependencyMgr(metrics),
		sigVerifierMgr:  sigVerifierMgr,
		shardsServerMgr: shardsServerMgr,
		outputChan:      make(chan []*TxStatus, defaultChannelBufferSize),
		stopSignalCh:    make(chan struct{}),
		metrics:         metrics,
	}
	c.startTxProcessingRoutine()
	c.startTxValidationProcessorRoutine()
	return c, nil
}

func (c *Coordinator) SetSigVerificationKey(k *sigverification.Key) error {
	return c.sigVerifierMgr.setVerificationKey(k)
}

func (c *Coordinator) ProcessBlockAsync(block *token.Block) {
	c.dependencyMgr.inputChan <- block
	c.sigVerifierMgr.inputChan <- block
	if c.metrics.Enabled {
		c.metrics.DependencyMgrInputChLength.Set(len(c.dependencyMgr.inputChan))
		c.metrics.SigVerifierMgrInputChLength.Set(len(c.sigVerifierMgr.inputChan))
		c.metrics.CoordinatorInTxs.Add(float64(len(block.Txs)))
	}
}

func (c *Coordinator) TxStatusChan() <-chan []*TxStatus {
	return c.outputChan
}

func (c *Coordinator) Stop() {
	c.dependencyMgr.stop()
	c.sigVerifierMgr.stop()
	c.shardsServerMgr.stop()
	close(c.stopSignalCh)
}

func (c *Coordinator) startTxProcessingRoutine() {
	remainings := []TxSeqNum{}

	sendDependencyFreeTxsToShardsServers := func(sigVerifiedTxs []TxSeqNum) {
		intersection, leftover := c.dependencyMgr.fetchDependencyFreeTxsThatIntersect(sigVerifiedTxs)
		remainings = leftover
		if len(intersection) > 0 {
			c.shardsServerMgr.inputChan <- intersection
			if c.metrics.Enabled {
				c.metrics.DependencyMgrOutTxs.Add(float64(len(intersection)))
				c.metrics.ShardMgrInputChLength.Set(len(c.shardsServerMgr.inputChan))
			}
		}
		if c.metrics.Enabled {
			c.metrics.SigVerifiedPendingTxs.Set(float64(len(remainings)))
		}
	}

	go func() {
		for {
			select {
			case <-c.stopSignalCh:
				return
			case sigVerifiedTxs := <-c.sigVerifierMgr.outputChanValids:
				sendDependencyFreeTxsToShardsServers(append(sigVerifiedTxs, remainings...))
				if c.metrics.Enabled {
					c.metrics.SigVerifierMgrValidOutputChLength.Set(len(c.sigVerifierMgr.outputChanValids))
				}
			case <-time.After(1 * time.Millisecond):
				if len(remainings) > 0 {
					sendDependencyFreeTxsToShardsServers(remainings)
				}
			}
		}
	}()
}

func (c *Coordinator) startTxValidationProcessorRoutine() {
	go func() {
		for {
			select {
			case <-c.stopSignalCh:
				return
			case status := <-c.shardsServerMgr.outputChan:
				c.processValidationStatus(status)
				if c.metrics.Enabled {
					c.metrics.ShardMgrOutputChLength.Set(len(c.shardsServerMgr.outputChan))
				}
			case invalids := <-c.sigVerifierMgr.outputChanInvalids:
				invalidStatus := make([]*TxStatus, len(invalids))
				for i := 0; i < len(invalids); i++ {
					invalidStatus[i] = &TxStatus{
						TxSeqNum: invalids[i],
						IsValid:  false,
					}
				}
				c.processValidationStatus(invalidStatus)
				if c.metrics.Enabled {
					c.metrics.SigVerifierMgrInvalidOutputChLength.Set(len(c.sigVerifierMgr.outputChanInvalids))
				}
			}
		}
	}()
}

func (c *Coordinator) processValidationStatus(txStatus []*TxStatus) {
	c.outputChan <- txStatus
	c.dependencyMgr.inputChanStatusUpdate <- txStatus
	if c.metrics.Enabled {
		c.metrics.CoordinatorOutTxs.Add(float64(len(txStatus)))
		c.metrics.DependencyMgrStatusUpdateChLength.Set(len(c.dependencyMgr.inputChanStatusUpdate))
	}
}
