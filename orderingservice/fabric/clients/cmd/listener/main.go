// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/hyperledger/fabric-config/protolator"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func main() {
	clients.SetEnvVars()
	defaults := clients.GetDefaultSecurityOpts()

	var (
		serverAddr string
		channelID  string
		quiet      bool
		seek       int
	)

	flag.StringVar(&serverAddr, "server", ":7050", "The RPC server to connect to.")
	flag.StringVar(&channelID, "channelID", "mychannel", "The channel ID to deliver from.")
	flag.BoolVar(&quiet, "quiet", false, "Only print the block number, will not attempt to print its block contents.")
	flag.IntVar(&seek, "seek", -2, fmt.Sprintf("Specify the range of requested blocks."+
		"Acceptable values:"+
		"%d (or %d) to start from oldest (or newest) and keep at it indefinitely."+
		"N >= 0 to fetch block N only.", clients.SeekSinceOldestBlock, clients.SeekSinceNewestBlock))
	flag.Parse()

	listener, err := clients.NewFabricOrdererListener(&clients.FabricOrdererConnectionOpts{
		ChannelID:              channelID,
		Endpoint:               *connection.CreateEndpoint(serverAddr),
		SecurityConnectionOpts: defaults,
	})

	if err != nil {
		return
	}

	start := time.Now()
	totalTxs := uint64(0)
	go func() {
		printer := message.NewPrinter(language.German)
		for {
			<-time.After(2 * time.Second)
			printer.Printf("Throughput: %d TXs/sec\n", totalTxs*uint64(time.Second)/uint64(time.Since(start)))
		}
	}()

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
			totalTxs += uint64(len(t.Block.Data.Data))
		}
	}))
}
