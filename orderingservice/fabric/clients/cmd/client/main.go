package main

import (
	"flag"
	"fmt"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

func main() {
	defaults := clients.GetDefaultConfigValues()
	opts := &sidecar.ClientInitOptions{
		OrdererTransportCredentials: defaults.Credentials,
		OrdererSigner:               defaults.Signer,
		InputChannelCapacity:        20,
	}
	var messages uint64

	connection.EndpointVar(&opts.SidecarEndpoint, "sidecar", *connection.CreateEndpoint(":1234"), "Endpoint where we listen for final committed blocks.")
	connection.EndpointVars(&opts.OrdererEndpoints, "orderers", []*connection.Endpoint{{"localhost", 7050}, {"localhost", 7051}, {"localhost", 7052}}, "Orderers to send our TXs.")
	flag.StringVar(&opts.ChannelID, "channelID", defaults.ChannelID, "The channel ID to broadcast to.")
	flag.IntVar(&opts.Parallelism, "goroutines", 3, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&messages, "messages", 1000, "The number of messages to broadcast.")
	config.ParseFlags()

	client, err := sidecar.NewClient(opts)
	utils.Must(err)
	defer func() {
		utils.Must(client.Close())
	}()

	done := make(chan struct{})
	client.StartListening(func(block *common.Block) {
		fmt.Printf("Block received %d\n", block.Header.Number)
	}, func(err error) {
		close(done)
	})

	client.SendBulk(func(i, _ int) (*token.Tx, bool) {
		return &token.Tx{SerialNumbers: [][]byte{[]byte("abcd")}}, i < int(messages)
	}).Wait()
	fmt.Printf("Sent %d messages.\n", messages)

	<-done
}
