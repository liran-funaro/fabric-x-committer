package main

import (
	"flag"
	"fmt"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

func main() {
	clients.SetEnvVars()
	defaults := clients.GetDefaultSecurityOpts()
	opts := &sidecar.ClientInitOptions{
		OrdererSecurityOpts:  defaults,
		InputChannelCapacity: 20,
	}
	profile := &workload.TransactionProfile{
		Size:          []test.DiscreteValue{{10, 1}},
		SignatureType: signature.Ecdsa,
	}
	var messages uint64

	connection.EndpointVar(&opts.CommitterEndpoint, "committer", *connection.CreateEndpoint(":5002"), "Endpoint of the committer to set the public key.")
	connection.EndpointVar(&opts.SidecarEndpoint, "sidecar", *connection.CreateEndpoint(":1234"), "Endpoint where we listen for final committed blocks.")
	connection.EndpointVars(&opts.OrdererEndpoints, "orderers", []*connection.Endpoint{{"localhost", 7050}, {"localhost", 7051}, {"localhost", 7052}}, "Orderers to send our TXs.")
	flag.StringVar(&opts.ChannelID, "channelID", "mychannel", "The channel ID to broadcast to.")
	flag.IntVar(&opts.Parallelism, "goroutines", 3, "The number of concurrent go routines to broadcast the messages on")
	flag.Uint64Var(&messages, "messages", 1000, "The number of messages to broadcast.")
	config.ParseFlags()

	publicKey, _, txs := workload.StartTxGenerator(profile, 100)

	client, err := sidecar.NewClient(opts)
	utils.Must(err)
	defer func() {
		utils.Must(client.Close())
	}()

	utils.Must(client.SetCommitterKey(publicKey))

	done := make(chan struct{})
	client.StartListening(func(block *common.Block) {
		fmt.Printf("Block received %d\n", block.Header.Number)
	}, func(err error) {
		close(done)
	})

	client.SendReplicated(func() (*token.Tx, bool) {
		tx, ok := <-txs
		return tx, ok
	}).Wait()
	fmt.Printf("Sent %d messages.\n", messages)

	<-done
}
