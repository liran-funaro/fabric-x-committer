// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/hyperledger/fabric-config/protolator"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric/clients/cmd"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
)

func main() {

	var (
		serverAddr     connection.Endpoint
		prometheusAddr connection.Endpoint
		credsPath      string
		configPath     string
		channelID      string
		quiet          bool
		seek           int
	)

	connection.EndpointVar(&serverAddr, "server", connection.Endpoint{"0.0.0.0", 7050}, "The RPC server to connect to.")
	connection.EndpointVar(&prometheusAddr, "prometheus-endpoint", connection.Endpoint{"0.0.0.0", 2112}, "Prometheus endpoint.")
	flag.StringVar(&channelID, "channelID", "mychannel", "The channel ID to deliver from.")
	flag.StringVar(&credsPath, "credsPath", connection.DefaultOutPath, "The path to the output folder containing the root CA and the client credentials.")
	flag.StringVar(&configPath, "configPath", connection.DefaultConfigPath, "The path to the output folder containing the orderer config.")
	flag.BoolVar(&quiet, "quiet", false, "Only print the block number, will not attempt to print its block contents.")
	flag.IntVar(&seek, "seek", -2, fmt.Sprintf("Specify the range of requested blocks."+
		"Acceptable values:"+
		"%d (or %d) to start from oldest (or newest) and keep at it indefinitely."+
		"N >= 0 to fetch block N only.", sidecar.SeekSinceOldestBlock, sidecar.SeekSinceNewestBlock))
	flag.Parse()

	creds, signer := connection.GetDefaultSecurityOpts(credsPath, configPath)

	listener, err := sidecar.NewFabricOrdererListener(&sidecar.FabricOrdererConnectionOpts{
		ChannelID:   channelID,
		Endpoint:    serverAddr,
		Credentials: creds,
		Signer:      signer,
	})
	if err != nil {
		return
	}

	m := cmd.LaunchSimpleThroughputMetrics(prometheusAddr, "listener", metrics.In)

	utils.Must(listener.RunOrdererOutputListenerForBlock(seek, func(msg *ab.DeliverResponse) {
		switch t := msg.Type.(type) {
		case *ab.DeliverResponse_Status:
			fmt.Println("Got status ", t)
			return
		case *ab.DeliverResponse_Block:
			if !quiet {
				fmt.Println("Received block: ")
				err := protolator.DeepMarshalJSON(os.Stdout, t.Block)
				if err != nil {
					fmt.Printf("  Error pretty printing block: %s", err)
				}
			} else {
				fmt.Printf("Received block: %d (size=%d) (tx count=%d; tx size=%d)\n", t.Block.Header.Number, t.Block.XXX_Size(), len(t.Block.Data.Data), len(t.Block.Data.Data[0]))
			}
			m.Throughput.Add(len(t.Block.Data.Data))
		}
	}))
}
