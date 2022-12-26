package clients

import (
	"context"
	"flag"
	"math"

	cb "github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients/pkg/identity"
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

//FabricOrdererListener connects to an orderer and listens for one or more blocks
type FabricOrdererListener struct {
	deliverClient *deliverClient
}

func NewFabricOrdererListener(opts *FabricOrdererConnectionOpts) (*FabricOrdererListener, error) {
	conn, err := connect(opts.Endpoint, opts.Credentials)
	client, err := ab.NewAtomicBroadcastClient(conn).Deliver(context.TODO())
	if err != nil {
		logger.Errorf("Error connecting: %v", err)
		return nil, err
	}

	return &FabricOrdererListener{newDeliverClient(client, opts.ChannelID, opts.Signer)}, nil
}

func (l *FabricOrdererListener) RunOrdererOutputListenerForBlock(seek int, onReceive func(*ab.DeliverResponse)) error {
	logger.Infof("Connecting to orderer output with seek = %d.\n", seek)
	var err error
	if seek < -2 {
		flag.PrintDefaults()
		err = errors.New("wrong seek value")
	} else if seek == SeekSinceOldestBlock {
		err = l.deliverClient.seekOldest()
	} else if seek == SeekSinceNewestBlock {
		err = l.deliverClient.seekNewest()
	} else {
		err = l.deliverClient.seekSingle(uint64(seek))
	}

	if err != nil {
		return err
	}

	l.deliverClient.readUntilClose(onReceive)
	return nil
}

func (l *FabricOrdererListener) RunOrdererOutputListener(onReceive func(*ab.DeliverResponse)) error {
	return l.RunOrdererOutputListenerForBlock(SeekSinceOldestBlock, onReceive)
}

type deliverClient struct {
	client    DeliverClient
	channelID string
	signer    identity.SignerSerializer
}

func newDeliverClient(client DeliverClient, channelID string, signer identity.SignerSerializer) *deliverClient {
	return &deliverClient{client: client, channelID: channelID, signer: signer}
}

func (r *deliverClient) seekHelper(start *ab.SeekPosition, stop *ab.SeekPosition) *cb.Envelope {
	env, err := protoutil.CreateSignedEnvelope(cb.HeaderType_DELIVER_SEEK_INFO, r.channelID, r.signer, &ab.SeekInfo{
		Start:    start,
		Stop:     stop,
		Behavior: ab.SeekInfo_BLOCK_UNTIL_READY,
	}, 0, 0)
	if err != nil {
		panic(err)
	}
	return env
}

func (r *deliverClient) seekOldest() error {
	return r.seekBetween(oldest, maxStop)
}

func (r *deliverClient) seekNewest() error {
	return r.seekBetween(newest, maxStop)
}

func (r *deliverClient) seekSingle(blockNumber uint64) error {
	specific := &ab.SeekPosition{Type: &ab.SeekPosition_Specified{Specified: &ab.SeekSpecified{Number: blockNumber}}}
	return r.seekBetween(specific, specific)
}

func (r *deliverClient) seekBetween(start, end *ab.SeekPosition) error {
	return r.client.Send(r.seekHelper(start, end))
}

func (r *deliverClient) readUntilClose(onReceive func(*ab.DeliverResponse)) {
	for {
		msg, err := r.client.Recv()
		if err != nil {
			logger.Errorf("Error receiving: %v", err)
			return
		}
		onReceive(msg)
	}
}
