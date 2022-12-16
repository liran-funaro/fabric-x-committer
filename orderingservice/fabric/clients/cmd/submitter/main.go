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
	defaults := clients.GetDefaultConfigValues()

	var (
		//serverAddr string
		channelID  string
		messages   uint64
		goroutines uint64
		msgSize    uint64
	)

	//flag.String(&serverAddr, "server", conf.Endpoint.Address(), "The RPC server to connect to.")
	flag.StringVar(&channelID, "channelID", defaults.ChannelID, "The channel ID to broadcast to.")
	flag.Uint64Var(&messages, "messages", 1000, "The number of messages to broadcast.")
	flag.Uint64Var(&goroutines, "goroutines", 3, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&msgSize, "size", 160, "The size in bytes of the data section for the payload")
	flag.Parse()

	s, err := clients.NewMultiFabricOrdererSubmitter(&clients.MultiFabricOrdererSubmitterOpts{
		ChannelID:   channelID,
		Endpoints:   []*connection.Endpoint{{"localhost", 7050}, {"localhost", 7051}, {"localhost", 7052}},
		Credentials: defaults.Credentials,
		Signer:      defaults.Signer,
	})
	if err != nil {
		return
	}
	defer func() {
		utils.Must(s.Close())
	}()

	msgsPerGo := messages / goroutines
	roundMsgs := msgsPerGo * goroutines
	if roundMsgs != messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}

	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")

	msgData := make([]byte, msgSize)

	s.BroadcastRepeat(int(goroutines), int(msgsPerGo), msgData, func(err error) {
		if err == nil && bar != nil {
			bar.Add(1)
		}
	}).Wait()
	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
