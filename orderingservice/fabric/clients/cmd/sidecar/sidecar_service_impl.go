package main

import (
	"fmt"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/sidecar"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

type serviceImpl struct {
	ab.UnimplementedAtomicBroadcastServer
	opts *sidecar.InitOptions
}

func (i *serviceImpl) Deliver(stream ab.AtomicBroadcast_DeliverServer) error {
	s, err := sidecar.New(i.opts)
	if err != nil {
		return err
	}

	s.Start(func(commonBlock *common.Block) {
		fmt.Printf("Received complete block from committer: %d:%d.\n", commonBlock.Header.Number, len(commonBlock.Data.Data))
		utils.Must(stream.Send(&ab.DeliverResponse{Type: &ab.DeliverResponse_Block{commonBlock}}))
	})
	return nil
}

func (*serviceImpl) Broadcast(ab.AtomicBroadcast_BroadcastServer) error {
	panic("not implemented")
}
