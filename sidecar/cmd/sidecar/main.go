package main

import (
	"flag"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	options := &sidecar.InitOptions{
		OrdererTransportCredentials: insecure.NewCredentials(),
	}
	flag.StringVar(&options.ChannelID, "channel-id", "", "Channel ID")
	connection.EndpointVar(&options.CommitterEndpoint, "committer", connection.Endpoint{}, "Committer host to connect to")
	connection.EndpointVar(&options.OrdererEndpoint, "orderer", connection.Endpoint{}, "Orderer host to connect to")
	connection.EndpointVar(&options.OutputEndpoint, "output", connection.Endpoint{}, "Output client to send the results to")
	config.ParseFlags()

	s, err := sidecar.New(options)
	if err != nil {
		panic(err)
	}
	s.Start()
}
