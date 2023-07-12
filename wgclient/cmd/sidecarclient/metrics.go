package main

import (
	"time"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
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
	t.TxSentAt(txId(id), time.Now())
}
func (t *metricTracker) BlockReceived(block *common.Block) {
	blockReceivedAt := time.Now()
	statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
	for i, data := range block.Data.Data {
		if _, channelHeader, err := serialization.UnwrapEnvelope(data); err == nil {
			t.TxReceivedAt(txId(channelHeader.TxId), sidecar.StatusInverseMap[statusCodes[i]], blockReceivedAt)
		}
	}
}

type txId string

func (i txId) String() string {
	return string(i)
}
