package clients

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc/credentials/insecure"
)

type SidecarListener struct {
	stream DeliverClient
}

func NewSidecarListener(endpoint connection.Endpoint) (*SidecarListener, error) {
	clientConnection, err := connect(endpoint, insecure.NewCredentials())
	if err != nil {
		return nil, err
	}
	client := ab.NewAtomicBroadcastClient(clientConnection)
	stream, err := client.Deliver(context.Background())
	if err != nil {
		return nil, err
	}
	return &SidecarListener{stream}, nil
}

func (l *SidecarListener) StartListening(onBlock func(*common.Block), onError func(error)) {
	logger.Infof("Starting to listen on sidecar for committed blocks.\n")
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
