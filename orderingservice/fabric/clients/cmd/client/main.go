package main

import (
	"context"
	"fmt"

	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

func main() {
	var clientEndpoint connection.Endpoint
	connection.EndpointVar(&clientEndpoint, "client", *connection.CreateEndpoint(":1234"), "Endpoint where we listen for incoming blocks")
	config.ParseFlags()

	clientConnection, err := connection.Connect(connection.NewDialConfig(clientEndpoint))
	client := ab.NewAtomicBroadcastClient(clientConnection)
	stream, err := client.Deliver(context.Background())
	utils.Must(err)

	for {
		if response, err := stream.Recv(); err != nil {
			fmt.Printf("Received error: %v\n", err)
		} else if block, ok := response.Type.(*ab.DeliverResponse_Block); ok {
			fmt.Printf("Received block %d.\n", block.Block.Header.Number)
		} else {
			fmt.Printf("Received response: %v\n", response)
		}
	}
}
