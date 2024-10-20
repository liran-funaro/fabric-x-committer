package sidecar

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"golang.org/x/sync/errgroup"
)

var logger = logging.New("sidecar")

// Service is a relay service which relays the block from orderer to committer. Further,
// it aggregates the transaction status and forwards the validated block to clients who have
// registered on the ledger server.
type Service struct {
	ordererClient      *deliverclient.Receiver
	relay              *relay
	ledgerService      *ledger.Service
	blockToBeCommitted chan *common.Block
	committedBlock     chan *common.Block
}

// New creates a sidecar service.
func New(c *SidecarConfig) (*Service, error) {
	// 1. Fetch blocks from the ordering service.
	blockToBeCommitted := make(chan *common.Block, 100)
	ordererClient, err := deliverclient.New(c.Orderer, deliverclient.Orderer, blockToBeCommitted)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to orderer:%v: %w", c.Orderer.Endpoint, err)
	}

	// 2. Relay the blocks to committer and receive the transaction status.
	committedBlock := make(chan *common.Block, 100)
	relay := newRelay(c.Committer, blockToBeCommitted, committedBlock)

	// 3. Deliver the block with status to client.
	logger.Infof("Create ledger service at %v for channel %s\n", c.Server.Endpoint.Address(), c.Orderer.ChannelID)
	ledgerService, err := ledger.New(c.Orderer.ChannelID, c.Ledger.Path, committedBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to create ledger: %w", err)
	}

	return &Service{
		ordererClient:      ordererClient,
		relay:              relay,
		ledgerService:      ledgerService,
		blockToBeCommitted: blockToBeCommitted,
		committedBlock:     committedBlock,
	}, nil
}

// Run starts the sidecar service. The call to Run blocks until an error occurs or the context is canceled.
func (s *Service) Run(ctx context.Context) error {
	g, eCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return s.ordererClient.Run(eCtx)
	})

	g.Go(func() error {
		return s.relay.Run(eCtx)
	})

	g.Go(func() error {
		return s.ledgerService.Run(eCtx)
	})

	if err := g.Wait(); err != nil {
		logger.Errorf("sidecar processing has been stopped due to err [%v]", err)
		return err
	}
	return nil
}

// GetLedgerService returns the ledger that implements peer.DeliverServer..
func (s *Service) GetLedgerService() *ledger.Service {
	return s.ledgerService
}
