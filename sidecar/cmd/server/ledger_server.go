package main

import (
	"github.com/hyperledger/fabric-protos-go/peer"
	"io"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric/common/ledger/blockledger"
	"github.com/hyperledger/fabric/common/ledger/blockledger/fileledger"
	"github.com/hyperledger/fabric/common/metrics/disabled"
	"github.com/hyperledger/fabric/common/util"
	"github.com/pkg/errors"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/serialization"
)

type ledgerDeliverServer struct {
	ab.UnimplementedAtomicBroadcastServer
	streams   []ab.AtomicBroadcast_DeliverServer
	mu        *sync.RWMutex
	input     chan *common.Block
	ledger    blockledger.ReadWriter
	channelId string
}

func newLedgerDeliverServer(channelId, ledgerDir string) deliverServer {
	factory, err := fileledger.New(ledgerDir, &disabled.Provider{})
	utils.Must(err)
	ledger, err := factory.GetOrCreate(channelId)
	utils.Must(err)
	i := &ledgerDeliverServer{
		streams:   make([]ab.AtomicBroadcast_DeliverServer, 0),
		mu:        &sync.RWMutex{},
		input:     make(chan *common.Block, 100),
		ledger:    ledger,
		channelId: channelId,
	}

	go func() {
		for {
			commonBlock := <-i.input
			utils.Must(i.ledger.Append(commonBlock))
		}
	}()
	return i
}

func (i *ledgerDeliverServer) Input() chan<- *common.Block {
	return i.input
}

func (i *ledgerDeliverServer) Deliver(srv peer.Deliver_DeliverServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())
	logger.Debugf("Starting new deliver loop for %s", addr)
	for {
		logger.Debugf("Attempting to read seek info message from %s", addr)
		envelope, err := srv.Recv()
		if err == io.EOF {
			logger.Infof("Received EOF from %s,", addr)
			return nil
		}
		if err != nil {
			return err
		}

		status, err := i.deliverBlocks(addr, srv, envelope)
		if err != nil {
			return err
		}

		err = srv.Send(&peer.DeliverResponse{
			Type: &peer.DeliverResponse_Status{Status: status},
		})
		if status != common.Status_SUCCESS {
			return err
		}
		if err != nil {
			logger.Infof("Error sending to %s: %s", addr, err)
			return err
		}

		logger.Debugf("Waiting for new SeekInfo from %s", addr)
	}
}

func (i *ledgerDeliverServer) deliverBlocks(addr string, srv peer.Deliver_DeliverServer, envelope *common.Envelope) (common.Status, error) {

	payload, chdr, err := serialization.ParseEnvelope(envelope)
	if err != nil {
		logger.Infof("error parsing envelope from %s: %s", addr, err)
		return common.Status_BAD_REQUEST, nil
	}

	if chdr.ChannelId != i.channelId {
		// Note, we log this at DEBUG because SDKs will poll waiting for channels to be created
		// So we would expect our log to be somewhat flooded with these
		logger.Debugf("Rejecting deliver for %s because channel %s not found", addr, chdr.ChannelId)
		return common.Status_NOT_FOUND, nil
	}

	cursor, stopNum, err := i.getCursor(payload.Data)
	if err != nil {
		logger.Infof("bad request: %v", err)
		return common.Status_BAD_REQUEST, nil
	}
	logger.Debugf("Received seekInfo.")

	for {
		block, status := cursor.Next()

		if status != common.Status_SUCCESS {
			logger.Errorf("error reading from channel: %v", status)
			return status, nil
		}

		if err := srv.Send(&peer.DeliverResponse{Type: &peer.DeliverResponse_Block{Block: block}}); err != nil {
			logger.Infof("internal server error occurred %s", err)
			return common.Status_INTERNAL_SERVER_ERROR, err
		}

		if stopNum == block.Header.Number {
			break
		}
	}

	logger.Debugf("Done delivering to %s", addr)

	return common.Status_SUCCESS, nil
}

func (i *ledgerDeliverServer) getCursor(payload []byte) (blockledger.Iterator, uint64, error) {
	seekInfo := &ab.SeekInfo{}
	if err := proto.Unmarshal(payload, seekInfo); err != nil {
		return nil, 0, errors.New("malformed seekInfo payload")
	}
	if seekInfo.Start == nil || seekInfo.Stop == nil {
		return nil, 0, errors.New("seekInfo missing start or stop")
	}

	cursor, number := i.ledger.Iterator(seekInfo.Start)

	switch stop := seekInfo.Stop.Type.(type) {
	case *ab.SeekPosition_Oldest:
		return cursor, number, nil
	case *ab.SeekPosition_Newest:
		// when seeking only the newest block (i.e. starting
		// and stopping at newest), don't reevaluate the ledger
		// height as this can lead to multiple blocks being
		// sent when only one is expected
		if proto.Equal(seekInfo.Start, seekInfo.Stop) {
			return cursor, number, nil
		}
		return cursor, i.ledger.Height() - 1, nil
	case *ab.SeekPosition_Specified:

		if stop.Specified.Number < number {
			return nil, 0, errors.New("start number greater than stop number")
		}
		return cursor, stop.Specified.Number, nil
	default:
		panic("unknown type")
	}
}
