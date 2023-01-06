package main

import (
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func main() {
	creds, signer := clients.GetDefaultSecurityOpts()

	config.ServerConfig("sidecar")
	config.String("channel-id", "sidecar.orderer.channel-id", "Channel ID")
	config.ParseFlags()

	c := sidecar.ReadConfig()

	m := metrics.New(c.Prometheus.IsEnabled())

	monitoring.LaunchPrometheus(c.Prometheus, monitoring.Sidecar, m)

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, &serviceImpl{ordererConfig: c.Orderer, committerConfig: c.Committer, credentials: creds, signer: signer, metrics: m})
	})

}

type serviceImpl struct {
	ab.UnimplementedAtomicBroadcastServer
	ordererConfig   *sidecar.OrdererClientConfig
	committerConfig *sidecar.CommitterClientConfig
	credentials     credentials.TransportCredentials
	signer          msp.SigningIdentity
	metrics         *metrics.Metrics
}

func (i *serviceImpl) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	s, err := sidecar.New(i.ordererConfig, i.committerConfig, i.credentials, i.signer, i.metrics)
	if err != nil {
		return err
	}

	s.Start(func(commonBlock *common.Block) {
		stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{commonBlock}})
	})
	return nil
}

func (*serviceImpl) Broadcast(ab.AtomicBroadcast_BroadcastServer) error {
	panic("not implemented")
}
