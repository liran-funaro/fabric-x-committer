package main

import (
	"flag"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

func main() {
	defaults := clients.GetDefaultConfigValues()
	options := &sidecar.InitOptions{
		OrdererTransportCredentials:    defaults.Credentials,
		CommitterOutputChannelCapacity: 20,
		OrdererSigner:                  defaults.Signer,
	}
	var output connection.Endpoint
	flag.StringVar(&options.ChannelID, defaults.ChannelID, defaults.ChannelID, "Channel ID")
	connection.EndpointVar(&options.CommitterEndpoint, "committer", *connection.CreateEndpoint(":5002"), "Committer host to connect to")
	connection.EndpointVar(&options.OrdererEndpoint, "orderer", *connection.CreateEndpoint("localhost:7051"), "Orderer host to connect to")
	connection.EndpointVar(&output, "output", *connection.CreateEndpoint("localhost:1234"), "Output client to send the results to")
	config.ParseFlags()

	connection.RunServerMain(&connection.ServerConfig{Endpoint: output}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, &serviceImpl{opts: options})
	})

}
