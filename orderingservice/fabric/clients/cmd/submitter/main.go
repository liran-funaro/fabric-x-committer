// Copyright IBM Corp. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"fmt"
	"sync"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/orderingservice/fabric/clients/cmd"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/sidecarclient"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	var (
		ordererEndpoints []*connection.Endpoint
		prometheusAddr   connection.Endpoint
		credsPath        string
		configPath       string
		rootCAPath       string
		localMspDir      string
		localMspId       string
		channelID        string
		messages         uint64
		goroutines       uint64
		msgSize          uint64
		signedEnvs       bool
	)

	connection.EndpointVars(&ordererEndpoints, "orderers", []*connection.Endpoint{{"0.0.0.0", 7050}, {"0.0.0.0", 7051}, {"0.0.0.0", 7052}}, "Orderers to send our TXs.")
	connection.EndpointVar(&prometheusAddr, "prometheus-endpoint", connection.Endpoint{"0.0.0.0", 2112}, "Prometheus endpoint.")
	flag.StringVar(&credsPath, "creds-path", connection.DefaultCredsPath, "The path to the output folder containing the root CA and the client credentials.")
	flag.StringVar(&configPath, "config-path", connection.DefaultConfigPath, "The path to the output folder containing the orderer config.")
	flag.StringVar(&rootCAPath, "root-ca-path", connection.DefaultRootCAPath, "The path to the root CA.")
	flag.StringVar(&localMspDir, "msp-dir", connection.DefaultLocalMspDir, "Local MSP Dir.")
	flag.StringVar(&localMspId, "msp-id", connection.DefaultLocalMspId, "Local MSP ID.")
	flag.StringVar(&channelID, "channel-id", "mychannel", "The channel ID to broadcast to.")
	flag.Uint64Var(&messages, "messages", 100_000, "The number of messages to broadcast.")
	flag.Uint64Var(&goroutines, "goroutines", 3, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&msgSize, "size", 160, "The size in bytes of the data section for the payload")
	flag.BoolVar(&signedEnvs, "signed", true, "Sign envelopes to send to orderer")
	flag.Parse()

	creds, signer := connection.GetDefaultSecurityOpts(credsPath, configPath, rootCAPath, localMspDir, localMspId)

	msgsPerGo := messages / goroutines
	roundMsgs := msgsPerGo * goroutines
	bar := workload.NewProgressBar("Submitting transactions...", int64(roundMsgs), "tx")
	opts := &sidecarclient.FabricOrdererBroadcasterOpts{
		Endpoints:            ordererEndpoints,
		Credentials:          creds,
		Parallelism:          int(goroutines),
		InputChannelCapacity: 10,
		OnAck: func(err error) {
			if err == nil && bar != nil {
				bar.Add(1)
			}
		},
	}

	m := cmd.LaunchSimpleThroughputMetrics(prometheusAddr, "submitter", metrics.Out)

	s, err := sidecarclient.NewFabricOrdererBroadcaster(opts)
	utils.Must(err)
	if roundMsgs != messages {
		fmt.Println("Rounding messages to", roundMsgs)
	}

	fmt.Printf("Sending the same message to all servers.\n")
	message := make([]byte, msgSize)
	envelopeCreator := sidecarclient.NewEnvelopeCreator(channelID, signer, signedEnvs)
	env, _ := envelopeCreator.CreateEnvelope(message)

	serializedEnv, err := protoutil.Marshal(env)
	utils.Must(err)
	fmt.Printf("Message size: %d\n", len(serializedEnv))

	var wg sync.WaitGroup
	wg.Add(len(s.Streams()))
	for _, ch := range s.Streams() {
		input := ch.Input()
		go func(out chan<- *common.Envelope) {
			for i := uint64(0); i < msgsPerGo; i++ {
				input <- env
				m.Throughput.Add(1)
			}
			wg.Done()
		}(input)
	}

	wg.Wait()

	//for i := uint64(0); i < msgsPerGo; i++ {
	//	// TODO submit asynchronously
	//	for _, ch := range s.Streams() {
	//		ch.Input() <- env
	//	}
	//	m.Throughput.Add(len(s.Streams()))
	//}

	utils.Must(s.CloseStreamsAndWait())

	fmt.Printf("----------------------broadcast message finish-------------------------------")
}
