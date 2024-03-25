package main

import (
	"context"
	"fmt"
	"io"
	"sync"

	token "github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	protocoordinatorservice "github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type mockService struct {
	protocoordinatorservice.UnimplementedCoordinatorServer
	bQueue chan *token.Block
}

func (m *mockService) SetVerificationKey(c context.Context, k *sigverification.Key) (*protocoordinatorservice.Empty, error) {
	fmt.Printf("set key: %v\n", k)
	return &protocoordinatorservice.Empty{}, nil
}

func (m *mockService) BlockProcessing(stream protocoordinatorservice.Coordinator_BlockProcessingServer) error {
	rQueue := make(chan *token.Block, 1000)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for block := range rQueue {
			resp := &protocoordinatorservice.TxValidationStatusBatch{
				TxsValidationStatus: make([]*protocoordinatorservice.TxValidationStatus, len(block.Txs)),
			}

			for i, tx := range block.Txs {
				status := &protocoordinatorservice.TxValidationStatus{
					TxId:   tx.GetId(),
					Status: token.Status_COMMITTED,
				}

				// make the mock response a bit more interesting ....
				if i%3 == 0 {
					status.Status = token.Status_ABORTED_MVCC_CONFLICT
				} else if i%7 == 0 {
					status.Status = token.Status_ABORTED_SIGNATURE_INVALID
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

	c := coordinatorservice.ReadConfig()

	bQueue := make(chan *token.Block, 1000)
	connection.RunServerMain(c.ServerConfig, func(server *grpc.Server, port int) {
		if c.ServerConfig.Endpoint.Port == 0 {
			c.ServerConfig.Endpoint.Port = port
		}
		protocoordinatorservice.RegisterCoordinatorServer(server, &mockService{bQueue: bQueue})
	})
}
