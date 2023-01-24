package sidecar

import (
	"context"
	"flag"
	"math"
	"time"

	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/msp"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	SeekSinceOldestBlock = -2
	SeekSinceNewestBlock = -1
)

var (
	oldest  = &ab.SeekPosition{Type: &ab.SeekPosition_Oldest{Oldest: &ab.SeekOldest{}}}
	newest  = &ab.SeekPosition{Type: &ab.SeekPosition_Newest{Newest: &ab.SeekNewest{}}}
	maxStop = &ab.SeekPosition{Type: &ab.SeekPosition_Specified{Specified: &ab.SeekSpecified{Number: math.MaxUint64}}}
)

//fabricOrdererListener connects to an orderer and listens for one or more blocks
type fabricOrdererListener struct {
	connection *grpc.ClientConn
	signer     identity.SignerSerializer
	channelID  string
	reconnect  time.Duration
	startBlock int64
}

type FabricOrdererConnectionOpts struct {
	Credentials credentials.TransportCredentials
	Signer      msp.SigningIdentity
	ChannelID   string
	Endpoint    connection.Endpoint
	Reconnect   time.Duration
	StartBlock  int64
}

func NewFabricOrdererListener(opts *FabricOrdererConnectionOpts) (*fabricOrdererListener, error) {
	conn, err := connection.Connect(connection.NewDialConfigWithCreds(opts.Endpoint, opts.Credentials))
	if err != nil {
		return nil, err
	}
	return &fabricOrdererListener{conn, opts.Signer, opts.ChannelID, opts.Reconnect, 0}, nil
}

func (l *fabricOrdererListener) RunOrdererOutputListener(onReceive func(*cb.Block)) error {
	//TODO: We currently do not support SeekSinceOldestBlock with restarts of the orderers. All blocks we get are sent to the committer. If we start from the oldest block, then all blocks up to the current block height will be recognized as double spends by the committer.
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
	err = client.Send(seekSince(seekPosition(startBlockNumber), channelID, signer))
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

func seekPosition(startBlockNumber int64) *ab.SeekPosition {
	var startPosition *ab.SeekPosition
	if startBlockNumber < -2 {
		flag.PrintDefaults()
		panic(errors.New("wrong seek value"))
	} else if startBlockNumber == SeekSinceOldestBlock {
		startPosition = oldest
	} else if startBlockNumber == SeekSinceNewestBlock {
		startPosition = newest
	} else {
		startPosition = &ab.SeekPosition{Type: &ab.SeekPosition_Specified{Specified: &ab.SeekSpecified{Number: uint64(startBlockNumber)}}}
	}
	return startPosition
}

func seekSince(start *ab.SeekPosition, channelID string, signer identity.SignerSerializer) *cb.Envelope {
	env, err := protoutil.CreateSignedEnvelope(cb.HeaderType_DELIVER_SEEK_INFO, channelID, signer, &ab.SeekInfo{
		Start:    start,
		Stop:     maxStop,
		Behavior: ab.SeekInfo_BLOCK_UNTIL_READY,
	}, 0, 0)
	if err != nil {
		panic(err)
	}
	return env
}
