package main

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"google.golang.org/grpc"
)

var logger = logging.New("mockcoordinator")

func main() {
	config.ParseFlags()

	// todo set address
	address := "localhost:8812"

	listener, err := net.Listen("tcp", address)
	utils.Must(err)
	grpcServer := grpc.NewServer()
	protocoordinatorservice.RegisterCoordinatorServer(grpcServer, &serviceImpl{})

	logger.Infof("Start mock coordinator listing on %s\n", address)
	err = grpcServer.Serve(listener)
	utils.Must(err)
}

type serviceImpl struct {
	protocoordinatorservice.UnimplementedCoordinatorServer
}

func (s *serviceImpl) SetVerificationKey(c context.Context, k *protosigverifierservice.Key) (*protocoordinatorservice.Empty, error) {
	return &protocoordinatorservice.Empty{}, nil
}

func (s *serviceImpl) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {

	input := make(chan *protoblocktx.Block)
	defer close(input)

	go s.sendTxsValidationStatus(stream, input)

	// start listening
	for {
		block, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}

		logger.Debugf("Received block %d with %d transactions", block.Number, len(block.Txs))

		// send to the validation
		input <- block
	}
}

func (s *serviceImpl) sendTxsValidationStatus(stream protocoordinatorservice.Coordinator_BlockProcessingServer, input chan *protoblocktx.Block) {
	for scBlock := range input {
		batch := &protocoordinatorservice.TxValidationStatusBatch{
			TxsValidationStatus: make([]*protocoordinatorservice.TxValidationStatus, len(scBlock.GetTxs())),
		}

		for i, tx := range scBlock.GetTxs() {
			batch.TxsValidationStatus[i] = &protocoordinatorservice.TxValidationStatus{
				TxId:   tx.GetId(),
				Status: protoblocktx.Status_COMMITTED,
			}
		}

		// coordinator sends responses in multiple chunks (parts)
		numParts := 1 + rand.Intn(6)
		perPart := len(scBlock.GetTxs()) / numParts

		for i := 0; i < numParts; i++ {
			go func(i int) {
				r := 100 + rand.Intn(1000)
				time.Sleep(time.Duration(r) * time.Microsecond)

				lo := i * perPart
				hi := lo + perPart

				total := len(scBlock.GetTxs())
				b := &protocoordinatorservice.TxValidationStatusBatch{}
				// check if we have a rest
				if total-hi > 0 && total-hi < perPart {
					b.TxsValidationStatus = batch.TxsValidationStatus[lo:]
				} else {
					b.TxsValidationStatus = batch.TxsValidationStatus[lo:hi]
				}

				if err := stream.Send(b); err != nil {
					utils.Must(err)
				}
			}(i)
		}
	}
}
