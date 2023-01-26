package main

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type mockService struct {
	coordinatorservice.UnimplementedCoordinatorServer
	bQueue chan *token.Block
}

func (m *mockService) SetVerificationKey(c context.Context, k *sigverification.Key) (*coordinatorservice.Empty, error) {
	fmt.Printf("set key: %v\n", k)
	return &coordinatorservice.Empty{}, nil
}

func (m *mockService) BlockProcessing(stream coordinatorservice.Coordinator_BlockProcessingServer) error {

	rQueue := make(chan *token.Block, 1000)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for block := range rQueue {
			resp := &coordinatorservice.TxValidationStatusBatch{
				TxsValidationStatus: make([]*coordinatorservice.TxValidationStatus, len(block.Txs)),
			}

			for i, _ := range block.Txs {
				status := &coordinatorservice.TxValidationStatus{
					BlockNum: block.Number,
					TxNum:    uint64(i),
					Status:   coordinatorservice.Status_VALID,
				}

				// make the mock response a bit more interesting ....
				if i%3 == 0 {
					status.Status = coordinatorservice.Status_DOUBLE_SPEND
				} else if i%7 == 0 {
					status.Status = coordinatorservice.Status_INVALID_SIGNATURE
				}

				resp.TxsValidationStatus[i] = status

			}
			stream.Send(resp)
		}
	}()

	fmt.Printf("New Connection...\n")
	for {
		block, err := stream.Recv()
		if err == io.EOF {
			// end of stream
			fmt.Printf("BlockProcessing EOF\n")
			break
		}

		if err != nil {
			fmt.Printf("error: %v\n", err)
			break
		}

		rQueue <- block
	}
	close(rQueue)

	// wait until we pushed all responses back
	wg.Wait()

	return nil
}

func main() {
	fmt.Printf("start mock coordinator service. Config values (except endpoint) will be ignored...\n")
	config.ParseFlags()

	c := pipeline.ReadConfig()

	bQueue := make(chan *token.Block, 1000)
	connection.RunServerMain(c.Server, func(grpcServer *grpc.Server) {
		coordinatorservice.RegisterCoordinatorServer(grpcServer, &mockService{bQueue: bQueue})
	})
}
