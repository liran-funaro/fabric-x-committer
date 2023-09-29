package main

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
)

type metricTracker struct {
	*workload.MetricTracker
}

func newMetricTracker(p monitoring.Config) *metricTracker {
	return &metricTracker{workload.NewMetricTracker(p)}
}

func (t *metricTracker) TxSent(id string) {
	t.TxSentAt(id, time.Now())
}
func (t *metricTracker) BlockReceived(block *common.Block) {
	blockReceivedAt := time.Now()
	statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	for i, data := range block.Data.Data {
		if _, channelHeader, err := serialization.UnwrapEnvelope(data); err == nil {
			t.TxReceivedAt(channelHeader.TxId, aggregator.StatusInverseMap[statusCodes[i]], blockReceivedAt)
		}
	}
}
