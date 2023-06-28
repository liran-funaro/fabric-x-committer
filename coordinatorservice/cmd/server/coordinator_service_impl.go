package main

import (
	"context"
	"io"

	"github.ibm.com/decentralized-trust-research/scalable-committer/pipeline"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("coordinatorservice")

type serviceImpl struct {
	coordinatorservice.UnimplementedCoordinatorServer
	Coordinator *pipeline.Coordinator
}

func (s *serviceImpl) SetVerificationKey(c context.Context, k *sigverification.Key) (*coordinatorservice.Empty, error) {
	err := s.Coordinator.SetSigVerificationKey(k)
	return &coordinatorservice.Empty{}, err
}

func (s *serviceImpl) BlockProcessing(stream coordinatorservice.Coordinator_BlockProcessingServer) error {

	// TODO double check if we can simplify our life by using the request tracker in utils/connection/request_tracker.go
	var numTxReceived uint64

	ch := make(chan uint64)
	responsesDone := make(chan bool)

	go s.sendTxsValidationStatus(stream, ch, responsesDone)

	// start listening
	for {
		block, err := stream.Recv()
		if err == io.EOF {
			// end of stream
			logger.Infof("BlockProcessing EOF\n")
			break
		}

		if err != nil {
			logger.Infof("error while recieving block from stream: %v\n", err)
			break
		}
		logger.Debugf("Received block %d:%d.", block.Number, len(block.Txs))
		s.Coordinator.ProcessBlockAsync(block)
		numTxReceived += uint64(len(block.GetTxs()))
	}

	// now we tell our status sender that when to finish
	ch <- numTxReceived

	// wait until we pushed all responses back
	<-responsesDone

	return nil
}

func (s *serviceImpl) sendTxsValidationStatus(stream coordinatorservice.Coordinator_BlockProcessingServer, expectedCh chan uint64, done chan bool) {
	defer func() {
		done <- true
	}()

	// used to decided when to stop sending responses
	sent := int64(0)
	expected := int64(-1)

	statusChan := s.Coordinator.TxStatusChan()
	for {
		select {
		case e, ok := <-expectedCh:
			// check if someone tells use when to stop
			if ok {
				expected = int64(e)
				logger.Debugf("Let's come to an end! sent: %d expected: %d\n", sent, expected)
			}
		case txsStatus, more := <-statusChan:
			if !more {
				// no more coming in on statusChan
				return
			}

			batch := &coordinatorservice.TxValidationStatusBatch{}
			batch.TxsValidationStatus = make([]*coordinatorservice.TxValidationStatus, len(txsStatus))

			for i, txStatus := range txsStatus {
				batch.TxsValidationStatus[i] = &coordinatorservice.TxValidationStatus{
					BlockNum: txStatus.TxSeqNum.BlkNum,
					TxNum:    txStatus.TxSeqNum.TxNum,
					Status:   statusMapping(txStatus.Status),
				}
			}

			logger.Debugf("Returning batch of %d TXs.", len(batch.TxsValidationStatus))
			err := stream.Send(batch)
			if err != nil {
				logger.Infof("Error while sending tx txStatus batch: %v\n", err)
				return
			}

			// track sent responses
			sent += int64(len(txsStatus))

			if expected != -1 {
				logger.Debugf("%d / %d delivered\n", sent, expected)
			}

			// if we know when to stop
			if expected != -1 && sent >= expected {
				return
			}
		}
	}
}

func statusMapping(s pipeline.Status) coordinatorservice.Status {
	_, ok := coordinatorservice.Status_name[int32(s)]
	if !ok {
		return coordinatorservice.Status_UNKNOWN
	}

	return coordinatorservice.Status(s)
}
