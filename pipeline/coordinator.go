package pipeline

import (
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	metricUtils "github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

func newCoordinatorChannelBufferGauge(subComponent, channel string) *metricUtils.ChannelBufferGauge {
	return metricUtils.NewChannelBufferGauge("coordinator", subComponent, channel, defaultChannelBufferSize)
}

type Coordinator struct {
	dependencyMgr   *dependencyMgr
	sigVerifierMgr  *sigVerifierMgr
	shardsServerMgr *shardsServerMgr
	outputChan      chan []*TxStatus
	stopSignalCh    chan struct{}
}

func NewCoordinator(config *CoordinatorConfig) (*Coordinator, error) {
	sigVerifierMgr, err := newSigVerificationMgr(config.SigVerifiers)
	if err != nil {
		return nil, err
	}
	shardsServerMgr, err := newShardsServerMgr(config.ShardsServers)
	if err != nil {
		return nil, err
	}
	c := &Coordinator{
		dependencyMgr:   newDependencyMgr(),
		sigVerifierMgr:  sigVerifierMgr,
		shardsServerMgr: shardsServerMgr,
		outputChan:      make(chan []*TxStatus, defaultChannelBufferSize),
		stopSignalCh:    make(chan struct{}),
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
	if Config.Prometheus.Enabled {
		dependencyMgrInputChLength.Set(len(c.dependencyMgr.inputChan))
		sigVerifierMgrInputChLength.Set(len(c.sigVerifierMgr.inputChan))
		metrics.IncomingTxs.Add(float64(len(block.Txs)))
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
			if Config.Prometheus.Enabled {
				shardMgrInputChLength.Set(len(c.shardsServerMgr.inputChan))
			}
		}
		if Config.Prometheus.Enabled {
			metrics.SigVerifiedPendingTxs.Set(float64(len(remainings)))
		}
	}

	go func() {
		for {
			select {
			case <-c.stopSignalCh:
				return
			case sigVerifiedTxs := <-c.sigVerifierMgr.outputChanValids:
				sendDependencyFreeTxsToShardsServers(append(sigVerifiedTxs, remainings...))
				if Config.Prometheus.Enabled {
					sigVerifierMgrValidOutputChLength.Set(len(c.sigVerifierMgr.outputChanValids))
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
				if Config.Prometheus.Enabled {
					shardMgrOutputChLength.Set(len(c.shardsServerMgr.outputChan))
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
				if Config.Prometheus.Enabled {
					sigVerifierMgrInvalidOutputChLength.Set(len(c.sigVerifierMgr.outputChanInvalids))
				}
			}
		}
	}()
}

func (c *Coordinator) processValidationStatus(txStatus []*TxStatus) {
	c.outputChan <- txStatus
	c.dependencyMgr.inputChanStatusUpdate <- txStatus
	if Config.Prometheus.Enabled {
		metrics.ProcessedTxs.Add(float64(len(txStatus)))
		dependencyMgrStatusUpdateChLength.Set(len(c.dependencyMgr.inputChanStatusUpdate))
	}
}
