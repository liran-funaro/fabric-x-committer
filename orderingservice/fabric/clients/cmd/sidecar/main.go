package main

import (
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
)

func main() {
	clients.SetEnvVars()
	defaults := clients.GetDefaultSecurityOpts()

	config.ServerConfig("sidecar")
	config.String("channel-id", "sidecar.orderer.channel-id", "Channel ID")
	config.ParseFlags()

	c := sidecar.ReadConfig()

	m := metrics.New(c.Prometheus.IsEnabled())

	monitoring.LaunchPrometheus(c.Prometheus, monitoring.Sidecar, m)

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, &serviceImpl{ordererConfig: c.Orderer, committerConfig: c.Committer, securityConfig: defaults, metrics: m})
	})

}

type serviceImpl struct {
	ab.UnimplementedAtomicBroadcastServer
	ordererConfig   *sidecar.OrdererClientConfig
	committerConfig *sidecar.CommitterClientConfig
	securityConfig  *clients.SecurityConnectionOpts
	metrics         *metrics.Metrics
}

func (i *serviceImpl) Deliver(stream clients.DeliverServer) error {
	s, err := sidecar.New(i.ordererConfig, i.committerConfig, i.securityConfig, i.metrics)
	if err != nil {
		return err
	}

	s.Start(func(commonBlock *common.Block) {
		stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{commonBlock}})
	})
	return nil
}

func (*serviceImpl) Broadcast(server clients.BroadcastServer) error {
	panic("not implemented")
}
