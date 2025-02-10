package ledger

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/hyperledger/fabric-lib-go/common/metrics/disabled"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric/common/ledger/blockledger"
	"github.com/hyperledger/fabric/common/ledger/blockledger/fileledger"
	"github.com/hyperledger/fabric/common/util"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"google.golang.org/protobuf/proto"
)

var logger = logging.New("ledger")

// Service implements peer.DeliverServer.
type Service struct {
	ledger                       blockledger.ReadWriter
	ledgerProvider               blockledger.Factory
	channelID                    string
	committedBlock               <-chan *common.Block
	nextToBeCommittedBlockNumber uint64
}

// New creates a new ledger service.
func New(channelID, ledgerDir string, input <-chan *common.Block) (*Service, error) {
	logger.Infof("Create ledger files for channel %s under %s", channelID, ledgerDir)
	factory, err := fileledger.New(ledgerDir, &disabled.Provider{})
	if err != nil {
		return nil, err
	}

	ledger, err := factory.GetOrCreate(channelID)
	if err != nil {
		return nil, err
	}

	return &Service{
		ledger:                       ledger,
		ledgerProvider:               factory,
		channelID:                    channelID,
		committedBlock:               input,
		nextToBeCommittedBlockNumber: ledger.Height(),
	}, nil
}

// Run starts the ledger service. The call to Run blocks until an error occurs or the context is canceled.
func (s *Service) Run(ctx context.Context) error {
	inputBlock := channel.NewReader(ctx, s.committedBlock)
	for {
		block, ok := inputBlock.Read()
		if !ok {
			return nil
		}

		if block.Header.Number < s.nextToBeCommittedBlockNumber {
			// NOTE: The block store height can be greater than the state database
			//       height. This is because the last committed block number is
			//       updated in the state database periodically, while blocks are
			//       written to the block store immediately. Consequently, it's
			//       possible to receive a block that is already present in the
			//       block store when the sidecar recovers after a failure, or when the
			//       coordinator recovers after a failure.
			continue
		} else if block.Header.Number > s.nextToBeCommittedBlockNumber {
			return fmt.Errorf("block store expects block number [%d] but received a greater block number [%d]",
				s.nextToBeCommittedBlockNumber, block.Header.Number)
		}
		s.nextToBeCommittedBlockNumber++

		logger.Debugf("Appending block %d to ledger.\n", block.Header.Number)
		if err := s.ledger.Append(block); err != nil {
			return err
		}
		logger.Debugf("Appended block %d to ledger.\n", block.Header.Number)
	}
}

// WaitForReady wait for service to be ready to be exposed as gRPC service.
// If the context ended before the service is ready, returns false.
func (*Service) WaitForReady(context.Context) bool {
	return true
}

// Close releases the ledger directory.
func (s *Service) Close() {
	s.ledgerProvider.Close()
}

// Deliver delivers the requested blocks.
func (s *Service) Deliver(srv peer.Deliver_DeliverServer) error {
	addr := util.ExtractRemoteAddress(srv.Context())
	logger.Infof("Starting new deliver loop for %s", addr)
	for {
		logger.Infof("Attempting to read seek info message from %s", addr)
		envelope, err := srv.Recv()
		logger.Infof("Received seek info message from %s", addr)
		if errors.Is(err, io.EOF) {
			logger.Infof("Received EOF from %s,", addr)
			return nil
		}
		if err != nil {
			return err
		}

		status, err := s.deliverBlocks(srv, envelope)
		if err != nil {
			logger.Infof("Failed delivering to %s with status %v: %v", addr, status, err)
			return err
		}
		logger.Infof("Done delivering to %s", addr)

		if err = srv.Send(&peer.DeliverResponse{
			Type: &peer.DeliverResponse_Status{Status: status},
		}); err != nil {
			logger.Infof("Error sending to %s: %s", addr, err)
			return err
		}

		logger.Infof("Waiting for new SeekInfo from %s", addr)
	}
}

// DeliverFiltered implements an API in peer.DeliverServer.
// Deprecated: this method is implemented to have compatibility with Fabric so that the fabric smart client
// can easily integrate with both FabricX and Fabric. Eventually, this method will be removed.
func (*Service) DeliverFiltered(peer.Deliver_DeliverFilteredServer) error {
	return errors.New("method is deprecated")
}

// DeliverWithPrivateData implements an API in peer.DeliverServer.
// Deprecated: this method is implemented to have compatibility with Fabric so that the fabric smart client
// can easily integrate with both FabricX and Fabric. Eventually, this method will be removed.
func (*Service) DeliverWithPrivateData(peer.Deliver_DeliverWithPrivateDataServer) error {
	return errors.New("method is deprecated")
}

// GetBlockHeight returns the height of the block store, i.e., the last committed block + 1. The +1 is needed
// to include block 0 as well.
func (s *Service) GetBlockHeight() uint64 {
	return s.ledger.Height()
}

func (s *Service) deliverBlocks(
	srv peer.Deliver_DeliverServer,
	envelope *common.Envelope,
) (common.Status, error) {
	payload, chdr, err := serialization.ParseEnvelope(envelope)
	if err != nil {
		return common.Status_BAD_REQUEST, fmt.Errorf("error parsing envelope: %w", err)
	}

	if chdr.ChannelId != s.channelID {
		// Note, we log this at DEBUG because SDKs will poll waiting for channels to be created
		// So we would expect our log to be somewhat flooded with these
		return common.Status_NOT_FOUND, errors.New("channel not found")
	}

	cursor, stopNum, err := s.getCursor(payload.Data)
	if err != nil {
		return common.Status_BAD_REQUEST, err
	}
	logger.Debugf("Received seekInfo.")

	for srv.Context().Err() == nil {
		block, status := cursor.Next()

		if status != common.Status_SUCCESS {
			return status, errors.New("error reading from channel")
		}

		if err := srv.Send(&peer.DeliverResponse{Type: &peer.DeliverResponse_Block{Block: block}}); err != nil {
			return common.Status_INTERNAL_SERVER_ERROR, fmt.Errorf("error sending response: %w", err)
		}
		logger.Infof("Successfully sent block %d:%d to client.", block.Header.Number, len(block.Data.Data))

		if stopNum == block.Header.Number {
			break
		}
	}
	return common.Status_SUCCESS, nil
}

func (s *Service) getCursor(payload []byte) (blockledger.Iterator, uint64, error) {
	seekInfo := &ab.SeekInfo{}
	if err := proto.Unmarshal(payload, seekInfo); err != nil {
		return nil, 0, errors.New("malformed seekInfo payload")
	}
	if seekInfo.Start == nil || seekInfo.Stop == nil {
		return nil, 0, errors.New("seekInfo missing start or stop")
	}

	cursor, number := s.ledger.Iterator(seekInfo.Start)

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
		return cursor, s.ledger.Height() - 1, nil
	case *ab.SeekPosition_Specified:

		if stop.Specified.Number < number {
			return nil, 0, errors.New("start number greater than stop number")
		}
		return cursor, stop.Specified.Number, nil
	default:
		return nil, 0, errors.New("unknown type")
	}
}
