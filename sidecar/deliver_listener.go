package sidecar

import (
	"context"
	"time"

	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

//fabricDeliverListener connects to an orderer and listens for one or more blocks
type fabricDeliverListener struct {
	connection *grpc.ClientConn
	signer     identity.SignerSerializer
	channelID  string
	reconnect  time.Duration
	startBlock int64
}

type DeliverConnectionOpts struct {
	Credentials credentials.TransportCredentials
	Signer      msp.SigningIdentity
	ChannelID   string
	Endpoint    connection.Endpoint
	Reconnect   time.Duration
	StartBlock  int64
}

func NewDeliverListener(opts *DeliverConnectionOpts) (*fabricDeliverListener, error) {
	conn, err := connection.Connect(connection.NewDialConfigWithCreds(opts.Endpoint, opts.Credentials))
	if err != nil {
		return nil, err
	}
	return &fabricDeliverListener{conn, opts.Signer, opts.ChannelID, opts.Reconnect, 0}, nil
}

func (l *fabricDeliverListener) RunDeliverOutputListener(onReceive func(*cb.Block)) error {
	reconnectAttempts := 0
	for {
		err := openDeliverStream(l.connection, l.channelID, l.signer, l.startBlock, func(block *cb.Block) {
			l.startBlock = int64(block.Header.Number) + 1
			onReceive(block)
		})

		if l.reconnect < 0 {
			logger.Infof("Error: %v.\nNot reconnecting.", err)
			return err
		} else {
			reconnectAttempts += 1
			logger.Infof("Error: %v.\nReconnecting in %v. Attempt %d", err, l.reconnect, reconnectAttempts)
			<-time.After(l.reconnect)
		}
	}
}

func openDeliverStream(conn *grpc.ClientConn, channelID string, signer identity.SignerSerializer, startBlockNumber int64, onReceive func(*cb.Block)) error {
	logger.Infof("Opening stream to orderer: %v", conn.Target())
	client, err := ab.NewAtomicBroadcastClient(conn).Deliver(context.TODO())
	if err != nil {
		return err
	}
	logger.Infof("Opened stream to orderer.")

	logger.Infof("Sending seek request starting from block %d.", startBlockNumber)
	err = client.Send(SeekSince(startBlockNumber, channelID, signer))
	if err != nil {
		return err
	}
	logger.Infof("Seek request sent.")

	for {
		msg, err := client.Recv()
		if err != nil {
			return err
		}
		switch t := msg.Type.(type) {
		case *ab.DeliverResponse_Status:
			logger.Infof("Got status %v", t)
			break
		case *ab.DeliverResponse_Block:
			onReceive(t.Block)
		}
	}
}
