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
	config.String("channel-id", "sidecar.orderer.channel-id", "Channel ID")
	config.String("orderer-creds-path", "sidecar.orderer.creds-path", "The path to the output folder containing the root CA and the client credentials.")
	config.String("orderer-config-path", "sidecar.orderer.config-path", "The path to the output folder containing the orderer config.")
	config.String("orderer-msp-dir", "sidecar.orderer.msp-dir", "The local MSP dir.")
	config.String("orderer-msp-id", "sidecar.orderer.msp-id", "The local MSP ID.")
	config.ParseFlags()

	c := sidecar.ReadConfig()
	creds, signer := connection.GetDefaultSecurityOpts(c.Orderer.CredsPath, c.Orderer.ConfigPath, c.Orderer.CredsPath+"/ca.crt", c.Orderer.MspDir, c.Orderer.MspId)

	m := metrics.New(c.Prometheus.IsEnabled())

	monitoring.LaunchPrometheus(c.Prometheus, monitoring.Sidecar, m)

	service := newLedgerDeliverServer(c.Orderer.ChannelID, c.Committer.LedgerPath)
	go connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
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
