package sidecar

import (
	"context"
	"fmt"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"golang.org/x/sync/errgroup"
)

var logger = logging.New("sidecar")

// Service is a relay service which relays the block from orderer to committer. Further,
// it aggregates the transaction status and forwards the validated block to clients who have
// registered on the ledger server.
type Service struct {
	ordererClient      *broadcastdeliver.Client
	relay              *relay
	ledgerService      *ledger.Service
	blockToBeCommitted chan *common.Block
	committedBlock     chan *common.Block
	config             *Config
}

// New creates a sidecar service.
func New(c *Config) (*Service, error) {
	// 1. Fetch blocks from the ordering service.
	ordererClient, err := broadcastdeliver.New(&c.Orderer)
	if err != nil {
		return nil, fmt.Errorf("failed to create orderer client: %w", err)
	}

	// 2. Relay the blocks to committer and receive the transaction status.
	blockToBeCommitted := make(chan *common.Block, 100)
	committedBlock := make(chan *common.Block, 100)
	relayService := newRelay(&c.Committer, blockToBeCommitted, committedBlock)

	// 3. Deliver the block with status to client.
	logger.Infof("Create ledger service at %v for channel %s\n", c.Server.Endpoint.Address(), c.Orderer.ChannelID)
	ledgerService, err := ledger.New(c.Orderer.ChannelID, c.Ledger.Path, committedBlock)
	if err != nil {
		return nil, fmt.Errorf("failed to create ledger: %w", err)
	}
	return &Service{
		ordererClient:      ordererClient,
		relay:              relayService,
		ledgerService:      ledgerService,
		blockToBeCommitted: blockToBeCommitted,
		committedBlock:     committedBlock,
		config:             c,
	}, nil
}

// WaitForReady wait for sidecar to be ready to be exposed as gRPC service.
// If the context ended before the service is ready, returns false.
func (s *Service) WaitForReady(ctx context.Context) bool {
	return s.ledgerService.WaitForReady(ctx)
}

// Run starts the sidecar service. The call to Run blocks until an error occurs or the context is canceled.
func (s *Service) Run(ctx context.Context) error {
	logger.Infof("Create coordinator client and connect to %s\n", &s.config.Committer.Endpoint)
	conn, err := connection.Connect(connection.NewDialConfig(&s.config.Committer.Endpoint))
	if err != nil {
		return fmt.Errorf("failed to connect to coordinator: %w", err)
	}
	defer conn.Close() // nolint:errcheck
	logger.Infof("sidecar connected to coordinator at %s", &s.config.Committer.Endpoint)

	client := protocoordinatorservice.NewCoordinatorClient(conn)
	blkInfo, err := client.GetNextExpectedBlockNumber(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to fetch the next expected block number from coordinator: %w", err)
	}
	logger.Infof("next expected block number by coordinator is %d", blkInfo.GetNumber())

	g, eCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return s.ordererClient.Deliver(eCtx, &broadcastdeliver.DeliverConfig{
			StartBlkNum: int64(blkInfo.GetNumber()), // nolint:gosec
			OutputBlock: s.blockToBeCommitted,
		})
	})

	// NOTE: We are not checking the last committed block number in the
	//       block store managed by the sidecar. This is because we
	//       plan to remove the block store to make the sidecar stateless
	//       as part of issue #549. As a result, the block store could
	//       fall behind the coordinator's last committed block number.
	//       This behavior is acceptable because the recovery feature is
	//       not yet complete.
	g.Go(func() error {
		return s.relay.Run(eCtx, &relayRunConfig{client, blkInfo.GetNumber()})
	})

	g.Go(func() error {
		return s.ledgerService.Run(eCtx)
	})

	if err := g.Wait(); err != nil {
		if !connection.IsStreamContextEnd(err) {
			logger.Errorf("sidecar processing has been stopped due to err [%v]", err)
		} else {
			logger.Info("sidecar processing has been stopped due to context end")
		}
		return err
	}
	return nil
}

// Close closes the ledger.
func (s *Service) Close() {
	s.ledgerService.Close()
}

// GetLedgerService returns the ledger that implements peer.DeliverServer.
func (s *Service) GetLedgerService() *ledger.Service {
	return s.ledgerService
}
