package main

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/peer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/sidecarservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

func main() {

	// TODO in the future we may want to use cobra to be consistent with other apps, i.e., coordinator service ...

	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := sidecarservice.ReadConfig()

	service, err := sidecarservice.NewService(&c)
	utils.Must(err)

	ordererErrChan, coordinatorErrChan, aggErrChan, err := service.Start(context.Background())
	utils.Must(err)
	errChan := utils.Capture(
		utils.ServiceErrors(ordererErrChan, 1, "orderer"),
		utils.ServiceErrors(coordinatorErrChan, 1, "coordinator"),
		utils.ServiceErrors(aggErrChan, 1, "aggregator"),
	)

	go func() {
		connection.RunServerMain(c.Server, func(server *grpc.Server, port int) {
			peer.RegisterDeliverServer(server, service)
		})
	}()

	utils.Must(<-errChan)
}
