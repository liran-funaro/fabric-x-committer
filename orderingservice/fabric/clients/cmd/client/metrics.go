package main

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

type metricTracker struct {
	*workload.MetricTracker
}

func newMetricTracker(p monitoring.Config) *metricTracker {
	return &metricTracker{workload.NewMetricTracker(p)}
}

func (t *metricTracker) TxSent(id string) {
	t.TxSentAt(txId(id), time.Now())
}
func (t *metricTracker) BlockReceived(block *common.Block) {
	blockReceivedAt := time.Now()
	for _, data := range block.Data.Data {
		if _, channelHeader, err := serialization.UnwrapEnvelope(data); err == nil {
			t.TxReceivedAt(txId(channelHeader.TxId), coordinatorservice.Status_UNKNOWN, blockReceivedAt)
		}
	}
}

type txId string

func (i txId) String() string {
	return string(i)
}
