// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"

	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	clients.SetEnvVars()
	defaults := clients.GetDefaultSecurityOpts()

	var (
		ordererEndpoints []*connection.Endpoint
		channelID        string
		messages         uint64
		goroutines       uint64
		msgSize          uint64
		signedEnvs       bool
	)

	connection.EndpointVars(&ordererEndpoints, "orderers", []*connection.Endpoint{{"localhost", 7050}, {"localhost", 7051}, {"localhost", 7052}}, "Orderers to send our TXs.")
	flag.StringVar(&channelID, "channelID", "mychannel", "The channel ID to broadcast to.")
	flag.Uint64Var(&messages, "messages", 100_000, "The number of messages to broadcast.")
	flag.Uint64Var(&goroutines, "goroutines", 3, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&msgSize, "size", 160, "The size in bytes of the data section for the payload")
	flag.BoolVar(&signedEnvs, "signed", false, "Sign envelopes to send to orderer")
	flag.Parse()

	msgsPerGo := messages / goroutines
	roundMsgs := msgsPerGo * goroutines
	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")
	opts := &clients.FabricOrdererBroadcasterOpts{
		ChannelID:            channelID,
		Endpoints:            ordererEndpoints,
		SecurityOpts:         defaults,
		SignedEnvelopes:      signedEnvs,
		Parallelism:          int(goroutines),
		InputChannelCapacity: 10,
		OnAck: func(err error) {
			if err == nil && bar != nil {
				bar.Add(1)
			}
		},
	}

	s, err := clients.NewFabricOrdererBroadcaster(opts)
	utils.Must(err)
	if roundMsgs != messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}
	s.SendRepeated(make([]byte, msgSize), int(msgsPerGo)).Wait()
	utils.Must(s.Close())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
