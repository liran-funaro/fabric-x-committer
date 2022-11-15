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
	dependencyMgr                *dependencyMgr
	sigVerifierMgr               *sigVerifierMgr
	shardsServerMgr              *shardsServerMgr
	stopSignalCh                 chan struct{}
	shardRequestCutTimeout       time.Duration
	invalidSigResponseCutoffSize int
	metrics                      *metrics.Metrics
}

func NewCoordinator(sigVerifierMgrConfig *SigVerifierMgrConfig, shardsServerMgrConfig *ShardsServerMgrConfig, limitsConfig *LimitsConfig, metrics *metrics.Metrics) (*Coordinator, error) {
	logger.Infof("Starting coordinator with params:\n"+
		"\t %d Sig verifiers: %v\n"+
		"\t %d Shard servers: %v\n"+
		"\t Limits:\n"+
		"\t\t Shard request cut timeout: %v\n"+
		"\t\t Dependency graph update timeout: %v\n"+
		"\t\t Max dependency graph size: %d\n"+
		"\t Total metrics: %d\n", len(sigVerifierMgrConfig.Endpoints), sigVerifierMgrConfig.Endpoints, len(shardsServerMgrConfig.Servers), shardsServerMgrConfig.GetEndpoints(), limitsConfig.ShardRequestCutTimeout, limitsConfig.DependencyGraphUpdateTimeout, limitsConfig.MaxDependencyGraphSize, len(metrics.AllMetrics()))
	sigVerifierMgr, err := newSigVerificationMgr(sigVerifierMgrConfig, metrics)
	if err != nil {
		return nil, err
	}
	shardsServerMgr, err := newShardsServerMgr(shardsServerMgrConfig, metrics)
	if err != nil {
		return nil, err
	}
	c := &Coordinator{
		dependencyMgr:                newDependencyMgr(limitsConfig.MaxDependencyGraphSize, limitsConfig.DependencyGraphUpdateTimeout, metrics),
		sigVerifierMgr:               sigVerifierMgr,
		shardsServerMgr:              shardsServerMgr,
		stopSignalCh:                 make(chan struct{}),
		shardRequestCutTimeout:       limitsConfig.ShardRequestCutTimeout,
		invalidSigResponseCutoffSize: limitsConfig.InvalidSigBatchCutoffSize,
		metrics:                      metrics,
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
		for txNum := range block.GetTxs() {
			txId := TxSeqNum{block.Number, uint64(txNum)}
			c.metrics.RequestTracer.StartAt(txId, before)
			c.metrics.RequestTracer.AddEventAt(txId, "Sent to dependency manager", depMgrSent)
			c.metrics.RequestTracer.AddEventAt(txId, "Sent to sigver manager", sigVerMgrSent)
		}
		c.metrics.WaitingDepMgrIn.Observe(float64(depMgrSent.Sub(before)))
		c.metrics.WaitingSigVerMgrIn.Observe(float64(sigVerMgrSent.Sub(depMgrSent)))
		//c.metrics.PreSignatureLatency.Begin(block.Number, 1, sigVerMgrSent)
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
				sentToShardServer := time.Now()
				for tx := range intersection {
					c.metrics.RequestTracer.AddEventAt(tx, "Intersection calculated", intersectionCalculated)
					c.metrics.RequestTracer.AddEventAt(tx, "Sent to shard server", sentToShardServer)
					c.metrics.WaitingPhaseOneIn.Observe(waitingDuration)
					//	c.metrics.PrePhaseOneLatency.End(tx, intersectionCalculated)
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
						c.metrics.RequestTracer.AddEventAt(tx, "Received valid response from sigver manager", received)
						//	c.metrics.PostSignatureLatency.End(tx, received)
						//	c.metrics.PrePhaseOneLatency.Begin(tx, 1, received)
					}
					c.metrics.SigVerifierMgrValidOutputChLength.Set(len(c.sigVerifierMgr.outputChanValids))
				}
				sendDependencyFreeTxsToShardsServers(append(sigVerifiedTxs, remainings...))
			case <-time.After(c.shardRequestCutTimeout):
				if len(remainings) > 0 {
					sendDependencyFreeTxsToShardsServers(remainings)
				}
			}
		}
	}()
}

func (c *Coordinator) startTxValidationProcessorRoutine() {
	invalidTxs := make([]*TxStatus, 0, c.invalidSigResponseCutoffSize*2)

	go func() {
		for {
			select {
			case <-c.stopSignalCh:
				return
			case status := <-c.shardsServerMgr.outputChan:

				invalidTxs = append(invalidTxs, status...)
				if len(invalidTxs) < c.invalidSigResponseCutoffSize {
					continue
				}

				if c.metrics.Enabled {
					received := time.Now()
					for _, tx := range status {
						c.metrics.RequestTracer.AddEventAt(tx.TxSeqNum, "Received response from shards server manager", received)
						c.metrics.PhaseOneLatency.End(tx.TxSeqNum, received)
						//c.metrics.StatusProcessLatency.Begin(tx.TxSeqNum, 1, received)
					}
					c.metrics.ShardMgrOutputChLength.Set(len(c.shardsServerMgr.outputChan))
				}
				if len(invalidTxs) > 0 {
					status = append(status, invalidTxs...)
					invalidTxs = make([]*TxStatus, 0, 2*c.invalidSigResponseCutoffSize)
				}
				c.processValidationStatus(status)
			case invalids := <-c.sigVerifierMgr.outputChanInvalids:
				for i := 0; i < len(invalids); i++ {
					invalidTxs = append(invalidTxs, &TxStatus{
						TxSeqNum: invalids[i],
						Status:   INVALID_SIGNATURE,
					})
				}

				if len(invalidTxs) < c.invalidSigResponseCutoffSize {
					continue
				}
				c.processValidationStatus(invalidTxs)
				if c.metrics.Enabled {
					received := time.Now()
					for _, tx := range invalidTxs {
						c.metrics.RequestTracer.AddEventAt(tx.TxSeqNum, "Received invalid response from sigver manager", received)
						//	c.metrics.PostSignatureLatency.End(tx.TxSeqNum, received)
					}
					c.metrics.SigVerifierMgrInvalidOutputChLength.Set(len(c.sigVerifierMgr.outputChanInvalids))
				}
				invalidTxs = make([]*TxStatus, 0, 2*c.invalidSigResponseCutoffSize)
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
