// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"

	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	var (
		ordererEndpoints []*connection.Endpoint
		credsPath        string
		configPath       string
		channelID        string
		messages         uint64
		goroutines       uint64
		msgSize          uint64
		signedEnvs       bool
	)

	connection.EndpointVars(&ordererEndpoints, "orderers", []*connection.Endpoint{{"0.0.0.0", 7050}, {"0.0.0.0", 7051}, {"0.0.0.0", 7052}}, "Orderers to send our TXs.")
	flag.StringVar(&credsPath, "credsPath", clients.DefaultOutPath, "The path to the output folder containing the root CA and the client credentials.")
	flag.StringVar(&configPath, "configPath", clients.DefaultConfigPath, "The path to the output folder containing the orderer config.")
	flag.StringVar(&channelID, "channelID", "mychannel", "The channel ID to broadcast to.")
	flag.Uint64Var(&messages, "messages", 100_000, "The number of messages to broadcast.")
	flag.Uint64Var(&goroutines, "goroutines", 3, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&msgSize, "size", 160, "The size in bytes of the data section for the payload")
	flag.BoolVar(&signedEnvs, "signed", true, "Sign envelopes to send to orderer")
	flag.Parse()

	creds, signer := clients.GetDefaultSecurityOpts(credsPath, configPath)

	msgsPerGo := messages / goroutines
	roundMsgs := msgsPerGo * goroutines
	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")
	opts := &sidecar.FabricOrdererBroadcasterOpts{
		ChannelID:            channelID,
		Endpoints:            ordererEndpoints,
		Credentials:          creds,
		Signer:               signer,
		SignedEnvelopes:      signedEnvs,
		Parallelism:          int(goroutines),
		InputChannelCapacity: 10,
		OnAck: func(err error) {
			if err == nil && bar != nil {
				bar.Add(1)
			}
		},
	}

	s, err := sidecar.NewFabricOrdererBroadcaster(opts)
	utils.Must(err)
	if roundMsgs != messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}

	fmt.Printf("Sending the same message to all servers.\n")
	message := make([]byte, msgSize)
	for i := uint64(0); i < msgsPerGo; i++ {
		for _, ch := range s.InputChannels() {
			ch <- message
		}
	}
	utils.Must(s.CloseAndWait())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
