package main

import (
	"fmt"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/metrics"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
)

type serviceImpl struct {
	ab.UnimplementedAtomicBroadcastServer
	ordererConfig   *sidecar.OrdererClientConfig
	committerConfig *sidecar.CommitterClientConfig
	securityConfig  *clients.SecurityConnectionOpts
	metrics         *metrics.Metrics
}

func (i *serviceImpl) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	s, err := sidecar.New(i.ordererConfig, i.committerConfig, i.securityConfig, i.metrics)
	if err != nil {
		return err
	}

	s.Start(func(commonBlock *common.Block) {
		fmt.Printf("Received complete block from committer: %d:%d.\n", commonBlock.Header.Number, len(commonBlock.Data.Data))
		stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{commonBlock}})
	})
	return nil
}

func (*serviceImpl) Broadcast(ab.AtomicBroadcast_BroadcastServer) error {
	panic("not implemented")
}
