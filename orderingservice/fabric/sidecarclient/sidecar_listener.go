package sidecarclient

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc/credentials/insecure"
)

//TODO: This request should contain a range of blocks we want to read. See comment in fabricOrdererListener.RunOrdererOutputListener.
var listenRequest = &common.Envelope{}

type sidecarListener struct {
	stream ab.AtomicBroadcast_DeliverClient
}

func newSidecarListener(endpoint connection.Endpoint) (*sidecarListener, error) {
	clientConnection, err := connection.Connect(connection.NewDialConfigWithCreds(endpoint, insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := ab.NewAtomicBroadcastClient(clientConnection)
	stream, err := client.Deliver(context.Background())
	if err != nil {
		return nil, err
	}
	return &sidecarListener{stream}, nil
}

func (l *sidecarListener) StartListening(onBlock func(*common.Block), onError func(error)) {
	logger.Infof("Starting to listen on sidecar for committed blocks.\n")
	utils.Must(l.stream.Send(listenRequest))
	go func() {
		for {
			if response, err := l.stream.Recv(); err != nil {
				onError(err)
			} else if block, ok := response.Type.(*ab.DeliverResponse_Block); ok {
				onBlock(block.Block)
			} else {
				logger.Infof("Received response: %v\n", response)
			}
		}
	}()
}
