package main

import (
	"flag"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	options := &sidecar.ClientInitOptions{
		OrdererTransportCredentials: insecure.NewCredentials(),
	}
	flag.StringVar(&options.ChannelID, "channel-id", "", "Channel ID")
	connection.EndpointVar(&options.OrdererEndpoint, "orderers", connection.Endpoint{}, "Orderer hosts to connect to")
	connection.EndpointVar(&options.ClientEndpoint, "client", connection.Endpoint{}, "Endpoint where we listen for incoming blocks")
	config.ParseFlags()

	c, err := sidecar.NewClient(options)
	if err != nil {
		panic(err)
	}
	c.Start()
}
