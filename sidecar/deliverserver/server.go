package deliverserver

import (
	"io"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	ab "github.com/hyperledger/fabric-protos-go/orderer"
	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/hyperledger/fabric/common/ledger/blockledger"
	"github.com/hyperledger/fabric/common/ledger/blockledger/fileledger"
	"github.com/hyperledger/fabric/common/metrics/disabled"
	"github.com/hyperledger/fabric/common/util"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

var logger = logging.New("ledger")

type Server interface {
	peer.DeliverServer
	Input() chan<- *common.Block
}

type ledgerServer struct {
	ab.UnimplementedAtomicBroadcastServer
	mu        *sync.RWMutex
	input     chan *common.Block
	ledger    blockledger.ReadWriter
	channelId string
}

func New(channelId, ledgerDir string) *ledgerServer {
	logger.Infof("Create ledger files for channel %s under %s", channelId, ledgerDir)
	factory, err := fileledger.New(ledgerDir, &disabled.Provider{})
	utils.Must(err)
	ledger, err := factory.GetOrCreate(channelId)
	utils.Must(err)
	srv := &ledgerServer{
		mu:        &sync.RWMutex{},
		input:     make(chan *common.Block, 100),
		ledger:    ledger,
		channelId: channelId,
	}

	go func() {
		for commonBlock := range srv.input {
			logger.Debugf("Appending block %d to ledger.\n", commonBlock.Header.Number)
			utils.Must(srv.ledger.Append(commonBlock))
			logger.Debugf("Appended block %d to ledger.\n", commonBlock.Header.Number)
		}
	}()
	return srv
}

func (i *ledgerServer) Input() chan<- *common.Block {
	return i.input
}

func (i *ledgerServer) Deliver(srv peer.Deliver_DeliverServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())
	logger.Infof("Starting new deliver loop for %s", addr)
	for {
		logger.Infof("Attempting to read seek info message from %s", addr)
		envelope, err := srv.Recv()
		logger.Infof("Received seek info message from %s", addr)
		if err == io.EOF {
			logger.Infof("Received EOF from %s,", addr)
			return nil
		}
		if err != nil {
			return err
		}

		status, err := i.deliverBlocks(srv, envelope)
		if err != nil {
			logger.Infof("Failed delivering to %s with status %v: %v", addr, status, err)
			return err
		}
		logger.Infof("Done delivering to %s", addr)

		err = srv.Send(&peer.DeliverResponse{
			Type: &peer.DeliverResponse_Status{Status: status},
		})

		if err != nil {
			logger.Infof("Error sending to %s: %s", addr, err)
			return err
		}

		logger.Infof("Waiting for new SeekInfo from %s", addr)
	}
}

func (i *ledgerServer) DeliverFiltered(server peer.Deliver_DeliverFilteredServer) error {
	//TODO implement me
	panic("implement me")
}

func (i *ledgerServer) DeliverWithPrivateData(server peer.Deliver_DeliverWithPrivateDataServer) error {
	//TODO implement me
	panic("implement me")
}

func (i *ledgerServer) deliverBlocks(srv peer.Deliver_DeliverServer, envelope *common.Envelope) (common.Status, error) {

	payload, chdr, err := serialization.ParseEnvelope(envelope)
	if err != nil {
		return common.Status_BAD_REQUEST, errors.Wrap(err, "error parsing envelope")
	}

	if chdr.ChannelId != i.channelId {
		// Note, we log this at DEBUG because SDKs will poll waiting for channels to be created
		// So we would expect our log to be somewhat flooded with these
		return common.Status_NOT_FOUND, errors.New("channel not found")
	}

	cursor, stopNum, err := i.getCursor(payload.Data)
	if err != nil {
		return common.Status_BAD_REQUEST, err
	}
	logger.Debugf("Received seekInfo.")

	for {
		block, status := cursor.Next()

		if status != common.Status_SUCCESS {
			return status, errors.New("error reading from channel")
		}

		if err := srv.Send(&peer.DeliverResponse{Type: &peer.DeliverResponse_Block{Block: block}}); err != nil {
			return common.Status_INTERNAL_SERVER_ERROR, errors.Wrap(err, "error sending response")
		}
		logger.Infof("Successfully sent block %d:%d to client.", block.Header.Number, len(block.Data.Data))

		if stopNum == block.Header.Number {
			break
		}
	}
	return common.Status_SUCCESS, nil
}

func (i *ledgerServer) getCursor(payload []byte) (blockledger.Iterator, uint64, error) {
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
		return nil, 0, errors.New("unknown type")
	}
}
