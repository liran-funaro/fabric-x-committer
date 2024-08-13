package main

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/peer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	// TODO in the future we may want to use cobra to be consistent with other apps, i.e., coordinator service ...
	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := sidecar.ReadConfig()

	service, err := sidecar.New(&c)
	utils.Must(err)

	ordererErrChan, coordinatorErrChan, aggErrChan, err := service.Start(context.Background())
	utils.Must(err)
	errChan := utils.Capture(
		utils.ServiceErrors(ordererErrChan, 1, "orderer"),
		utils.ServiceErrors(coordinatorErrChan, 1, "coordinator"),
		utils.ServiceErrors(aggErrChan, 1, "aggregator"),
	)

	// enable health check server
	healthcheck := health.NewServer()
	healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)

	go func() {
		connection.RunServerMain(c.Server, func(server *grpc.Server, _ int) {
			peer.RegisterDeliverServer(server, service.LedgerService)
			healthgrpc.RegisterHealthServer(server, healthcheck)
		})
	}()

	utils.Must(<-errChan)
}
