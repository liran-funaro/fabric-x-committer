package main

import (
	"fmt"
	"sync"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/common/util"
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
	config.ServerConfig("sidecar")
	config.String("channel-id", "sidecar.orderer.channel-id", "Channel ID")
	config.String("orderer-creds-path", "sidecar.orderer.creds-path", "The path to the output folder containing the root CA and the client credentials.")
	config.String("orderer-config-path", "sidecar.orderer.config-path", "The path to the output folder containing the orderer config.")
	config.ParseFlags()

	c := sidecar.ReadConfig()
	creds, signer := clients.GetDefaultSecurityOpts(c.Orderer.CredsPath, c.Orderer.ConfigPath)

	m := metrics.New(c.Prometheus.IsEnabled())

	monitoring.LaunchPrometheus(c.Prometheus, monitoring.Sidecar, m)

	connection.RunServerMain(&connection.ServerConfig{Endpoint: c.Endpoint}, func(grpcServer *grpc.Server) {
		ab.RegisterAtomicBroadcastServer(grpcServer, newSidecarService(c.Orderer, c.Committer, creds, signer, m))
	})

}

type serviceImpl struct {
	ab.UnimplementedAtomicBroadcastServer
	streams []ab.AtomicBroadcast_DeliverServer
	mu      *sync.RWMutex
}

func newSidecarService(ordererConfig *sidecar.OrdererClientConfig, committerConfig *sidecar.CommitterClientConfig, credentials credentials.TransportCredentials, signer msp.SigningIdentity, metrics *metrics.Metrics) *serviceImpl {
	s, err := sidecar.New(ordererConfig, committerConfig, credentials, signer, metrics)
	if err != nil {
		panic(err)
	}

	i := &serviceImpl{
		streams: make([]ab.AtomicBroadcast_DeliverServer, 0),
		mu:      &sync.RWMutex{},
	}

	_ = s
	go s.Start(func(commonBlock *common.Block) {
		fmt.Printf("Sending out block %v to %d clients\n", commonBlock.Header.Number, len(i.streams))
		response := &ab.DeliverResponse{Type: &ab.DeliverResponse_Block{commonBlock}}
		i.mu.RLock()
		for _, stream := range i.streams {
			_ = stream.Send(response)
		}
		i.mu.RUnlock()
	})
	return i
}

func (i *serviceImpl) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	address := util.ExtractRemoteAddress(stream.Context())
	fmt.Printf("Opening new stream: %s\n", address)
	for {
		if _, err := stream.Recv(); err == nil {
			if i.findStreamIndex(stream) < 0 {
				fmt.Printf("Adding stream: %s\n", address)
				i.addStream(stream)
			} else {
				fmt.Printf("Cannot add stream: %s\n", address)
			}
		} else {
			fmt.Printf("Error occurred: %v\n", err)
			if index := i.findStreamIndex(stream); index >= 0 {
				fmt.Printf("Removing stream: %s\n", address)
				i.removeStream(index)
			} else {
				fmt.Printf("Could not remove stream: %s\n", address)
			}
			return err
		}
	}
}

func (i *serviceImpl) addStream(stream ab.AtomicBroadcast_DeliverServer) {
	i.mu.Lock()
	i.streams = append(i.streams, stream)
	i.mu.Unlock()
}

func (i *serviceImpl) removeStream(index int) {
	i.mu.Lock()
	i.streams[index] = i.streams[len(i.streams)-1]
	i.streams = i.streams[:len(i.streams)-1]
	i.mu.Unlock()
}

func (i *serviceImpl) findStreamIndex(needle ab.AtomicBroadcast_DeliverServer) int {
	address := util.ExtractRemoteAddress(needle.Context())
	i.mu.RLock()
	defer i.mu.RUnlock()
	for i, stream := range i.streams {
		if util.ExtractRemoteAddress(stream.Context()) == address {
			return i
		}
	}
	return -1
}

func (*serviceImpl) Broadcast(ab.AtomicBroadcast_BroadcastServer) error {
	panic("not implemented")
}
