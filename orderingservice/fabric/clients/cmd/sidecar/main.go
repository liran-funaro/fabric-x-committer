package main

import (
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

func main() {
	clients.SetEnvVars()
	defaults := clients.GetDefaultSecurityOpts()

	c := sidecar.ReadConfig()

	config.ServerConfig("sidecar")
	config.String("channel-id", "sidecar.orderer.channel-id", "Channel ID")
	config.ParseFlags()

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, &serviceImpl{ordererConfig: c.Orderer, committerConfig: c.Committer, securityConfig: defaults})
	})

}
