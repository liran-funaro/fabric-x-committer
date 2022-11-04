package pipeline

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

var logger = logging.New("coordiantor")

type Coordinator struct {
	dependencyMgr   *dependencyMgr
	sigVerifierMgr  *sigVerifierMgr
	shardsServerMgr *shardsServerMgr
	stopSignalCh    chan struct{}
	metrics         *metrics.Metrics
}

func NewCoordinator(sigVerifierMgrConfig *SigVerifierMgrConfig, shardsServerMgrConfig *ShardsServerMgrConfig, metrics *metrics.Metrics) (*Coordinator, error) {
	logger.Infof("Starting coordinator with params:\n"+
		"\t %d Sig verifiers: %v\n"+
		"\t %d Shard servers: %v\n"+
		"\t Total metrics: %d\n", len(sigVerifierMgrConfig.Endpoints), sigVerifierMgrConfig.Endpoints, len(shardsServerMgrConfig.Servers), shardsServerMgrConfig.GetEndpoints(), len(metrics.AllMetrics()))
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
	before := time.Now()
	c.dependencyMgr.inputChan <- block
	depMgrSent := time.Now()
	c.sigVerifierMgr.inputChan <- block
	sigVerMgrSent := time.Now()
	if c.metrics.Enabled {
		c.metrics.WaitingDepMgrIn.Observe(float64(depMgrSent.Sub(before)))
		c.metrics.WaitingSigVerMgrIn.Observe(float64(sigVerMgrSent.Sub(depMgrSent)))
		c.metrics.PreSignatureLatency.Begin(block.Number, 1, sigVerMgrSent)
		c.metrics.DependencyMgrInputChLength.Set(len(c.dependencyMgr.inputChan))
		c.metrics.SigVerifierMgrInputChLength.Set(len(c.sigVerifierMgr.inputChan))
		c.metrics.CoordinatorInTxs.Add(len(block.Txs))
	}
}

func (c *Coordinator) TxStatusChan() <-chan []*TxStatus {
	return c.dependencyMgr.outputChanStatusUpdate
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
			intersectionCalculated := time.Now()
			c.shardsServerMgr.inputChan <- intersection
			if c.metrics.Enabled {
				waitingDuration := float64(time.Now().Sub(intersectionCalculated))
				for tx := range intersection {
					c.metrics.WaitingPhaseOneIn.Observe(waitingDuration)
					c.metrics.PrePhaseOneLatency.End(tx, intersectionCalculated)
					c.metrics.PhaseOneLatency.Begin(tx, 1, intersectionCalculated)
				}
				c.metrics.DependencyMgrOutTxs.Add(len(intersection))
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
				if c.metrics.Enabled {
					received := time.Now()
					for _, tx := range sigVerifiedTxs {
						c.metrics.PostSignatureLatency.End(tx, received)
						c.metrics.PrePhaseOneLatency.Begin(tx, 1, received)
					}
					c.metrics.SigVerifierMgrValidOutputChLength.Set(len(c.sigVerifierMgr.outputChanValids))
				}
				sendDependencyFreeTxsToShardsServers(append(sigVerifiedTxs, remainings...))
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
				if c.metrics.Enabled {
					received := time.Now()
					for _, tx := range status {
						c.metrics.PhaseOneLatency.End(tx.TxSeqNum, received)
						c.metrics.StatusProcessLatency.Begin(tx.TxSeqNum, 1, received)
					}
					c.metrics.ShardMgrOutputChLength.Set(len(c.shardsServerMgr.outputChan))
				}
				c.processValidationStatus(status)
			case invalids := <-c.sigVerifierMgr.outputChanInvalids:
				invalidStatus := make([]*TxStatus, len(invalids))
				for i := 0; i < len(invalids); i++ {
					invalidStatus[i] = &TxStatus{
						TxSeqNum: invalids[i],
						Status:   INVALID_SIGNATURE,
					}
				}
				c.processValidationStatus(invalidStatus)
				if c.metrics.Enabled {
					received := time.Now()
					for _, tx := range invalidStatus {
						c.metrics.PostSignatureLatency.End(tx.TxSeqNum, received)
					}
					c.metrics.SigVerifierMgrInvalidOutputChLength.Set(len(c.sigVerifierMgr.outputChanInvalids))
				}
			}
		}
	}()
}

func (c *Coordinator) processValidationStatus(txStatus []*TxStatus) {
	c.dependencyMgr.inputChanStatusUpdate <- txStatus
	if c.metrics.Enabled {
		c.metrics.CoordinatorOutTxs.Add(len(txStatus))
		c.metrics.DependencyMgrStatusUpdateChLength.Set(len(c.dependencyMgr.inputChanStatusUpdate))
	}
}
