// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/hyperledger/fabric-config/protolator"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/pflag"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric/clients/cmd"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	//var (
	//	ordererListenAddrs []*connection.Endpoint
	//	ordererOpsAddrs    []*connection.Endpoint
	//)
	//connection.EndpointVars(&ordererListenAddrs, "orderer-endpoints", []*connection.Endpoint{}, "The orderer listening endpoints to connect to.")
	//connection.EndpointVars(&ordererOpsAddrs, "orderer-ops-endpoints", []*connection.Endpoint{}, "The orderer operations endpoints to fetch prometheus metrics from.")

	quiet := pflag.Bool("quiet", false, "Only print the block number, will not attempt to print its block contents.")
	seek := pflag.Int64("seek", -2, fmt.Sprintf("Specify the range of requested blocks."+
		"Acceptable values:"+
		"%d (or %d) to start from oldest (or newest) and keep at it indefinitely."+
		"N >= 0 to fetch starting from block N.", deliver.SeekSinceOldestBlock, deliver.SeekSinceNewestBlock))
	config.ParseFlags()

	c := fabric.ReadListenerConfig()
	p := connection.ReadConnectionProfile(c.OrdererConnectionProfile)
	creds, signer := connection.GetOrdererConnectionCreds(p)

	//if len(ordererListenAddrs) > 0 {
	//	if len(ordererOpsAddrs) != len(ordererListenAddrs) {
	//		panic("not all endpoints given")
	//	}
	//	fmt.Println("Will look for a follower node to connect to.")
	//	leaderOrdererIdx, _, err := cmd.NewPrometheusMetricClient(c.OrdererConnectionProfile.ChannelID, c.OrdererConnectionProfile.RootCAPaths).GetLeader(ordererOpsAddrs, 2*time.Second)
	//	utils.Must(err)
	//	c.Orderer = *ordererListenAddrs[(leaderOrdererIdx+1)%len(ordererListenAddrs)]
	//	fmt.Printf("Leader orderer found: [%d] -> %s. Connecting to follower: %s.\n", leaderOrdererIdx, ordererListenAddrs[leaderOrdererIdx], c.Orderer.Address())
	//} else {
	//	fmt.Printf("No orderer listen/ops addresses passed. Will listen on %d.\n", c.Orderer.Address())
	//}

	listener, err := deliver.NewListener(&deliver.ConnectionOpts{
		ClientProvider: &sidecar.OrdererDeliverClientProvider{},
		ChannelID:      c.ChannelID,
		Endpoint:       c.Orderer,
		Credentials:    creds,
		Signer:         signer,
		Reconnect:      10 * time.Second,
		StartBlock:     *seek,
	})
	if err != nil {
		return
	}

	m := newListenerMetrics()
	monitoring.LaunchPrometheus(c.Prometheus, monitoring.Other, m)
	bar := workload.NewProgressBar("Received transactions...", -1, "tx")

	utils.Must(listener.RunDeliverOutputListener(func(block *common.Block) {
		if !*quiet {
			fmt.Println("Received block: ")
			err := protolator.DeepMarshalJSON(os.Stdout, block)
			if err != nil {
				fmt.Printf("  Error pretty printing block: %s", err)
			}
			//} else {
		}
		fmt.Printf("Received block: %d (size=%d) (tx count=%d; tx size=%d)\n", block.Header.Number, block.XXX_Size(), len(block.Data.Data), len(block.Data.Data[0]))
		blockSize := len(block.Data.Data)
		m.Throughput.Add(blockSize)
		m.BlockSizes.Set(float64(blockSize))
		bar.Add(blockSize)
	}))
}

func newListenerMetrics() *ListenerMetrics {
	return &ListenerMetrics{
		&cmd.ThroughputMetrics{metrics.NewThroughputCounter("listener", metrics.In)},
		prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "block_sizes",
			Help: "Sizes of incoming blocks",
		}, []string{"sub_component"}).With(prometheus.Labels{"sub_component": "listener"}),
	}
}

type ListenerMetrics struct {
	*cmd.ThroughputMetrics
	BlockSizes prometheus.Gauge
}

func (m *ListenerMetrics) AllMetrics() []prometheus.Collector {
	return append(m.ThroughputMetrics.AllMetrics(), m.BlockSizes)
}
