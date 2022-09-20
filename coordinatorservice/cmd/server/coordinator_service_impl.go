package main

import (
	"context"
	"fmt"

	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
)

type serviceImpl struct {
	coordinatorservice.UnimplementedCoordinatorServer
	Coordinator *pipeline.Coordinator
}

func (s *serviceImpl) SetVerificationKey(c context.Context, k *sigverification.Key) (*coordinatorservice.Empty, error) {
	err := s.Coordinator.SetSigVerificationKey(k)
	return &coordinatorservice.Empty{}, err
}

func (s *serviceImpl) BlockProcessing(stream coordinatorservice.Coordinator_BlockProcessingServer) error {
	go s.sendTxsValidationStatus(stream)
	for {
		block, err := stream.Recv()
		if err != nil {
			panic(fmt.Sprintf("Error while recieving block from stream: %s", err))
		}
		s.Coordinator.ProcessBlockAsync(block)
	}
}

func (s *serviceImpl) sendTxsValidationStatus(stream coordinatorservice.Coordinator_BlockProcessingServer) {
	statusChan := s.Coordinator.TxStatusChan()
	for txsStatus := range statusChan {
		batch := &coordinatorservice.TxValidationStatusBatch{}
		batch.TxsValidationStatus = make([]*coordinatorservice.TxValidationStatus, len(txsStatus))

		for i, status := range txsStatus {
			batch.TxsValidationStatus[i] = &coordinatorservice.TxValidationStatus{
				BlockNum: status.TxSeqNum.BlkNum,
				TxNum:    status.TxSeqNum.BlkNum,
				IsValid:  status.IsValid,
			}
		}

		err := stream.Send(batch)
		if err != nil {
			panic(fmt.Sprintf("Error while sending tx status batch: %s", err))
		}
	}
}
