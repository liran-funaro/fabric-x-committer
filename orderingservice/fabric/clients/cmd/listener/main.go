// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hyperledger/fabric-config/protolator"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric/clients/cmd"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/deliver"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {

	var (
		serverAddr     connection.Endpoint
		prometheusAddr connection.Endpoint
		credsPath      string
		configPath     string
		rootCAPath     string
		localMspDir    string
		localMspId     string
		channelID      string
		quiet          bool
		seek           int64
	)

	connection.EndpointVar(&serverAddr, "server", connection.Endpoint{"0.0.0.0", 7050}, "The RPC server to connect to.")
	connection.EndpointVar(&prometheusAddr, "prometheus-endpoint", connection.Endpoint{"0.0.0.0", 2112}, "Prometheus endpoint.")
	flag.StringVar(&channelID, "channel-id", "mychannel", "The channel ID to deliver from.")
	flag.StringVar(&credsPath, "creds-path", connection.DefaultCredsPath, "The path to the output folder containing the msp directory with the client credentials.")
	flag.StringVar(&configPath, "config-path", connection.DefaultConfigPath, "The path to the output folder containing the orderer config.")
	flag.StringVar(&rootCAPath, "root-ca-path", connection.DefaultRootCAPath, "The path to the root CA.")
	flag.StringVar(&localMspDir, "msp-dir", connection.DefaultLocalMspDir, "Local MSP Dir.")
	flag.StringVar(&localMspId, "msp-id", connection.DefaultLocalMspId, "Local MSP ID.")
	flag.BoolVar(&quiet, "quiet", false, "Only print the block number, will not attempt to print its block contents.")
	flag.Int64Var(&seek, "seek", -2, fmt.Sprintf("Specify the range of requested blocks."+
		"Acceptable values:"+
		"%d (or %d) to start from oldest (or newest) and keep at it indefinitely."+
		"N >= 0 to fetch starting from block N.", deliver.SeekSinceOldestBlock, deliver.SeekSinceNewestBlock))
	flag.Parse()

	creds, signer := connection.GetDefaultSecurityOpts(credsPath, configPath, rootCAPath, localMspDir, localMspId)

	listener, err := deliver.NewListener(&deliver.ConnectionOpts{
		ClientProvider: &sidecar.OrdererDeliverClientProvider{},
		ChannelID:      channelID,
		Endpoint:       serverAddr,
		Credentials:    creds,
		Signer:         signer,
		Reconnect:      10 * time.Second,
		StartBlock:     seek,
	})
	if err != nil {
		return
	}

	m := cmd.LaunchSimpleThroughputMetrics(prometheusAddr, "listener", metrics.In)
	bar := workload.NewProgressBar("Received transactions...", -1, "tx")

	utils.Must(listener.RunDeliverOutputListener(func(block *common.Block) {
		if !quiet {
			fmt.Println("Received block: ")
			err := protolator.DeepMarshalJSON(os.Stdout, block)
			if err != nil {
				fmt.Printf("  Error pretty printing block: %s", err)
			}
		} else {
			//fmt.Printf("Received block: %d (size=%d) (tx count=%d; tx size=%d)\n", block.Header.Number, block.XXX_Size(), len(block.Data.Data), len(block.Data.Data[0]))
		}
		m.Throughput.Add(len(block.Data.Data))
		bar.Add(len(block.Data.Data))
	}))
}
