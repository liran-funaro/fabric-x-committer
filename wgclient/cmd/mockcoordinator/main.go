package main

import (
	"context"
	"fmt"
	"io"

	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/coordinatorservice"
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

	go func() {
		for block := range rQueue {
			resp := &coordinatorservice.TxValidationStatusBatch{
				TxsValidationStatus: make([]*coordinatorservice.TxValidationStatus, len(block.Txs)),
			}

			for i, _ := range block.Txs {
				status := &coordinatorservice.TxValidationStatus{
					BlockNum: block.Number,
					TxNum:    uint64(i),
					IsValid:  true,
				}
				resp.TxsValidationStatus[i] = status

				// TODO send some responses
				//stream.Send(resp)
			}
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

		// just spawn a responder
		go func(block *token.Block) {
			resp := &coordinatorservice.TxValidationStatusBatch{
				TxsValidationStatus: make([]*coordinatorservice.TxValidationStatus, len(block.Txs)),
			}

			for i, _ := range block.Txs {
				status := &coordinatorservice.TxValidationStatus{
					BlockNum: block.Number,
					TxNum:    uint64(i),
					IsValid:  true,
				}
				resp.TxsValidationStatus[i] = status
			}

			//stream.Send(resp)
		}(block)

	}
	close(rQueue)
	fmt.Printf("closing onnection...\n")

	return nil
}

func main() {
	fmt.Printf("start mock coordinator service ...\n")
	conf := &connection.ServerConfig{
		Endpoint: connection.Endpoint{
			Host: "0.0.0.0",
			Port: config.DefaultGRPCPortCoordinatorServer,
		},
		PrometheusEnabled: false,
		Opts:              nil,
	}

	fmt.Printf("listing on %s:%d\n", conf.Host, conf.Port)

	bQueue := make(chan *token.Block, 1000)
	connection.RunServerMain(conf, func(grpcServer *grpc.Server) {
		coordinatorservice.RegisterCoordinatorServer(grpcServer, &mockService{bQueue: bQueue})
	})
}
