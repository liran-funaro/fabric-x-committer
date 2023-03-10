package main

import (
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/sidecar/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
)

var logger = logging.New("server")

type deliverServer interface {
	Deliver(server peer.Deliver_DeliverServer) error
	Input() chan<- *common.Block
}

func main() {
	config.ServerConfig("sidecar")
	config.ParseFlags()

	c := sidecar.ReadConfig()
	p := connection.ReadConnectionProfile(c.Orderer.OrdererConnectionProfile)
	creds, signer := connection.GetOrdererConnectionCreds(p)

	m := monitoring.LaunchMonitoring(c.Monitoring, &metrics.Provider{}).(*metrics.Metrics)

	service := newLedgerDeliverServer(c.Orderer.ChannelID, c.Committer.LedgerPath)
	go connection.RunServerMain(c.Server, func(grpcServer *grpc.Server) {
		peer.RegisterDeliverServer(grpcServer, &serviceImpl{deliverDelegate: service})
	})

	s, err := sidecar.New(c.Orderer, c.Committer, creds, signer, m)
	utils.Must(err)

	s.Start(func(commonBlock *common.Block) {
		service.Input() <- commonBlock
	})
}

type serviceImpl struct {
	peer.UnimplementedDeliverServer
	deliverDelegate deliverServer
}

func (i *serviceImpl) Deliver(server peer.Deliver_DeliverServer) error {
	return i.deliverDelegate.Deliver(server)
}
