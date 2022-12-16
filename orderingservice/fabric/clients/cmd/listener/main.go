// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"os"

	"github.com/hyperledger/fabric-config/protolator"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

func main() {
	defaults := clients.GetDefaultConfigValues()

	var (
		serverAddr string
		channelID  string
		quiet      bool
		seek       int
	)

	flag.StringVar(&serverAddr, "server", defaults.Endpoint.Address(), "The RPC server to connect to.")
	flag.StringVar(&channelID, "channelID", defaults.ChannelID, "The channel ID to deliver from.")
	flag.BoolVar(&quiet, "quiet", false, "Only print the block number, will not attempt to print its block contents.")
	flag.IntVar(&seek, "seek", -2, fmt.Sprintf("Specify the range of requested blocks."+
		"Acceptable values:"+
		"%d (or %d) to start from oldest (or newest) and keep at it indefinitely."+
		"N >= 0 to fetch block N only.", clients.SeekSinceOldestBlock, clients.SeekSinceNewestBlock))
	flag.Parse()

	listener, err := clients.NewFabricOrdererListener(&clients.FabricOrdererConnectionOpts{
		ChannelID:   channelID,
		Endpoint:    *connection.CreateEndpoint(serverAddr),
		Credentials: defaults.Credentials,
		Signer:      defaults.Signer,
	})

	if err != nil {
		return
	}

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
		}
	}))
}
