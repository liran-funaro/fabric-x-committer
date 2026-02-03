/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package sidecar

import (
	"context"
	"fmt"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/hyperledger/fabric-x-common/api/committerpb"
	"github.com/hyperledger/fabric-x-common/tools/configtxgen"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/deliver"
	"github.com/hyperledger/fabric-x-committer/utils/logging"
	"github.com/hyperledger/fabric-x-committer/utils/monitoring/promutil"
	"github.com/hyperledger/fabric-x-committer/utils/ordererconn"
)

var logger = logging.New("sidecar")

// Service is a relay service which relays the block from orderer to committer. Further,
// it aggregates the transaction status and forwards the validated block to clients who have
// registered on the ledger server.
type Service struct {
	delivery           deliver.OrdererDeliveryParameters
	relay              *relay
	notifier           *notifier
	ledgerService      *ledgerService
	coordConn          *grpc.ClientConn
	blockToBeCommitted chan *common.Block
	committedBlock     chan *common.Block
	statusQueue        chan []*committerpb.TxStatus
	config             *Config
	healthcheck        *health.Server
	metrics            *perfMetrics
}

// New creates a sidecar service.
func New(c *Config) (*Service, error) {
	logger.Info("Initializing new sidecar")

	// 1. Fetch blocks from the ordering service.
	err := ordererconn.ValidateConfig(&c.Orderer)
	if err != nil {
		return nil, err
	}
	var lastConfigBlock *common.Block
	if c.Bootstrap.GenesisBlockFilePath != "" {
		lastConfigBlock, err = configtxgen.ReadBlock(c.Bootstrap.GenesisBlockFilePath)
		if err != nil {
			return nil, errors.Wrap(err, "read config block")
		}
	}

	// 2. Relay the blocks to committer and receive the transaction status.
	metrics := newPerformanceMetrics()
	relayService := newRelay(c.LastCommittedBlockSetInterval, metrics)

	// 3. Deliver the block with status to client.
	logger.Infof("Create ledger service for channel %s", c.Orderer.ChannelID)
	ledgerService, err := newLedgerService(c.Orderer.ChannelID, c.Ledger.Path, metrics)
	if err != nil {
		return nil, fmt.Errorf("failed to create ledger: %w", err)
	}

	bufferSize := c.ChannelBufferSize
	if bufferSize <= 0 {
		bufferSize = defaultBufferSize
	}
	blockToBeCommitted := make(chan *common.Block, bufferSize)
	return &Service{
		delivery: deliver.OrdererDeliveryParameters{
			FaultToleranceLevel:     c.Orderer.FaultToleranceLevel,
			TLS:                     c.Orderer.Connection.TLS,
			Retry:                   c.Orderer.Connection.Retry,
			Identity:                c.Orderer.Identity,
			BlockWithholdingTimeout: time.Second,
			LastestKnownConfig:      lastConfigBlock,
			OutputBlock:             blockToBeCommitted,
		},
		relay:              relayService,
		notifier:           newNotifier(bufferSize, &c.Notification),
		ledgerService:      ledgerService,
		healthcheck:        connection.DefaultHealthCheckService(),
		config:             c,
		metrics:            metrics,
		blockToBeCommitted: blockToBeCommitted,
		committedBlock:     make(chan *common.Block, bufferSize),
		statusQueue:        make(chan []*committerpb.TxStatus, bufferSize),
	}, nil
}

// WaitForReady wait for sidecar to be ready to be exposed as gRPC service.
// If the context ended before the service is ready, returns false.
func (*Service) WaitForReady(context.Context) bool {
	return true
}

// Run starts the sidecar service. The call to Run blocks until an error occurs or the context is canceled.
func (s *Service) Run(ctx context.Context) error {
	pCtx, pCancel := context.WithCancel(ctx)
	defer pCancel()
	// similar to other services, when the prometheus server returns an error, we do not terminate the service.
	go func() {
		_ = s.metrics.StartPrometheusServer(pCtx, s.config.Monitoring.Server, s.monitorQueues)
	}()

	logger.Infof("Create coordinator client and connect to %s", s.config.Committer.Endpoint)
	conn, connErr := connection.NewSingleConnection(s.config.Committer)
	if connErr != nil {
		return errors.Wrapf(connErr, "failed to connect to coordinator")
	}
	s.coordConn = conn
	defer connection.CloseConnectionsLog(conn)
	logger.Infof("sidecar connected to coordinator at %s", s.config.Committer.Endpoint)
	coordClient := servicepb.NewCoordinatorClient(conn)

	g, gCtx := errgroup.WithContext(pCtx)

	// The following runs independently of the coordinator connection lifecycle.
	// gCtx will be cancelled if these stopped processing due to ledger error.
	// Such errors require human interaction to resolve the ledger discrepancy.
	g.Go(func() error {
		// Deliver the block with status to clients.
		return s.ledgerService.run(gCtx, &ledgerRunConfig{
			IncomingCommittedBlock: s.committedBlock,
		})
	})
	g.Go(func() error {
		// Notification for clients.
		return s.notifier.run(gCtx, s.statusQueue)
	})

	g.Go(func() error {
		// TODO: initialize retry from config.
		return connection.Sustain(gCtx, nil, func() error {
			defer func() {
				s.recoverCommittedBlocks(gCtx)
				// We should drop all enqueued block if any.
				s.blockToBeCommitted = make(chan *common.Block, cap(s.blockToBeCommitted))
				s.delivery.OutputBlock = s.blockToBeCommitted
			}()
			return s.sendBlocksAndReceiveStatus(gCtx, coordClient)
		})
	})

	return utils.ProcessErr(g.Wait(), "sidecar has been stopped")
}

// RegisterService registers for the sidecar's GRPC services.
func (s *Service) RegisterService(server *grpc.Server) {
	peer.RegisterDeliverServer(server, s.ledgerService)
	committerpb.RegisterNotifierServer(server, s.notifier)
	healthgrpc.RegisterHealthServer(server, s.healthcheck)
}

func (s *Service) sendBlocksAndReceiveStatus(
	ctx context.Context,
	coordClient servicepb.CoordinatorClient,
) error {
	defer s.metrics.coordConnection.Disconnected(s.coordConn.CanonicalTarget())
	var err error
	err = s.recoverDeliveryFromLedgerStore(ctx, coordClient)
	if err != nil {
		return errors.Join(connection.ErrBackOff, err)
	}

	blkInfo, err := coordClient.GetNextBlockNumberToCommit(ctx, nil)
	if err != nil {
		return logAndWrapCoordinatorError(err, "failed to fetch the next expected block number from coordinator")
	}
	logger.Infof("next expected block number by coordinator is %d", blkInfo.Number)

	// if the recovery is successful, the connection is established.
	s.metrics.coordConnection.Connected(s.coordConn.CanonicalTarget())

	g, gCtx := errgroup.WithContext(ctx)

	// NOTE: deliver.OrdererToChannel and relay.Run must always return an error on exist.
	g.Go(func() error {
		logger.Info("Fetch blocks from the ordering service and write them on s.blockToBeCommitted.")
		deliverErr := deliver.OrdererToChannel(gCtx, &s.delivery)
		if errors.Is(deliverErr, context.Canceled) {
			// A context may be canceled due to a relay error, thus it is not critical error.
			return errors.Wrap(deliverErr, "context is canceled")
		}
		return errors.Join(connection.ErrNonRetryable, deliverErr)
	})

	g.Go(func() error {
		logger.Info("Relay the blocks to committer (from s.blockToBeCommitted) and receive the transaction status.")
		return s.relay.run(gCtx, &relayRunConfig{
			coordClient:                    coordClient,
			nextExpectedBlockByCoordinator: blkInfo.Number,
			incomingBlockToBeCommitted:     s.blockToBeCommitted,
			outgoingCommittedBlock:         s.committedBlock,
			outgoingStatusUpdates:          s.statusQueue,
			waitingTxsLimit:                s.config.WaitingTxsLimit,
		})
	})

	return g.Wait()
}

func (s *Service) recoverCommittedBlocks(ctx context.Context) {
	for ctx.Err() == nil && len(s.committedBlock) > 0 {
		logger.Infof("Waiting for committed block queue: %d", len(s.committedBlock))
		time.Sleep(100 * time.Millisecond)
	}
}

func (s *Service) recoverDeliveryFromLedgerStore(
	ctx context.Context, coordClient servicepb.CoordinatorClient,
) error {
	logger.Info("recovering sidecar")
	// If the sidecar fails but the coordinator remains active, the sidecar
	// must wait for the coordinator to become idle (i.e., to finish
	// processing all previously submitted transactions) before attempting
	// recovery. This ensures proper block store recovery in the sidecar.
	// For example, suppose blocks 100 through 200 were sent to the coordinator.
	// Some of these blocks might be partially committed, with the sidecar
	// having received partial status updates, when the sidecar crashes and
	// restarts. Upon restart, the sidecar can easily determine the
	// coordinator's next expected block. However, it needs to reconstruct
	// its block store to fill any gaps.
	// The sidecar can identify missing blocks using its current block store
	// height and the coordinator's next expected block number. It can then
	// fetch these missing blocks from the ordering service and their statuses
	// from the coordinator, finally committing them to its local block store.
	// However, if the coordinator has not fully committed these blocks,
	// the sidecar might not be able to retrieve all necessary status updates.
	// To prevent this, the sidecar waits for the coordinator to complete all
	// pending transactions before attempting recovery. This ensures that the
	// sidecar retrieves complete status information and avoids inconsistencies
	// in its block store.
	if err := waitForIdleCoordinator(ctx, coordClient); err != nil {
		return err
	}
	if s.ledgerService.ledger.Height() == 0 {
		return nil
	}

	latestBlock, err := s.ledgerService.getLatestBlock()
	if err != nil {
		return err
	}
	verificationConfigBlock, err := s.ledgerService.getConfigBlockOfBlock(latestBlock)
	if err != nil {
		return err
	}
	latestConfigBlock, err := s.ledgerService.getLatestConfigBlock()
	if err != nil {
		return err
	}

	s.delivery.NextBlockVerificationConfig = verificationConfigBlock
	s.delivery.LastBlock = latestBlock

	// We might have a newer config block than the one in the ledger
	// (e.g., from the config YAML or from previous delivery run).
	// This can help in cases where the sidecar was down too long and missed a crucial config-block
	// that updated all the endpoints, leaving no known orderers to fetch blocks from.
	if s.delivery.LastestKnownConfig != nil &&
		s.delivery.LastestKnownConfig.Header.Number < latestConfigBlock.Header.Number {
		s.delivery.LastestKnownConfig = latestConfigBlock
	}
	return nil
}

func (s *Service) monitorQueues(ctx context.Context) {
	ticker := time.NewTicker(100 * time.Millisecond)
	m := s.metrics
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		promutil.SetGauge(m.yetToBeCommittedBlocksQueueSize, len(s.blockToBeCommitted))
		promutil.SetGauge(m.committedBlocksQueueSize, len(s.committedBlock))
	}
}

// Close closes the ledger.
func (s *Service) Close() {
	s.ledgerService.close()
}

func waitForIdleCoordinator(ctx context.Context, client servicepb.CoordinatorClient) error {
	for {
		waitingTxs, err := client.NumberOfWaitingTransactionsForStatus(ctx, nil)
		if err != nil {
			return logAndWrapCoordinatorError(err, "failed to get number of waiting transactions from coordinator")
		}
		if waitingTxs.Count == 0 {
			return nil
		}
		logger.Infof("Waiting for coordinator to complete processing [%d] pending transactions", waitingTxs.Count)
		time.Sleep(100 * time.Millisecond)
	}
}

func fillStatuses(
	finalStatuses []committerpb.Status,
	statuses []*committerpb.TxStatus,
	expectedHeight []*committerpb.TxRef,
) error {
	// This copy to a map has a negligible performance impact is fine as this is a rare case.
	statusMap := make(map[string]*committerpb.TxStatus, len(statuses))
	for _, s := range statuses {
		statusMap[s.Ref.TxId] = s
	}
	for _, ref := range expectedHeight {
		s, ok := statusMap[ref.TxId]
		if !ok {
			return errors.Newf("committer should have the status of txID [%s] but it does not", ref.TxId)
		}
		if committerpb.AreSameHeight(ref, s.Ref) {
			finalStatuses[ref.TxNum] = s.Status
		} else {
			finalStatuses[ref.TxNum] = committerpb.Status_REJECTED_DUPLICATE_TX_ID
		}
	}
	return nil
}

// logAndWrapCoordinatorError logs the gRPC status code from a coordinator error and wraps it with context.
// This helps with debugging by providing visibility into the specific gRPC error codes returned by the coordinator.
func logAndWrapCoordinatorError(err error, contextMsg string) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if ok {
		logger.Errorf("%s: gRPC status code=%s, message=%s", contextMsg, st.Code(), st.Message())
	} else {
		logger.Errorf("%s: %v", contextMsg, err)
	}

	return errors.Wrap(err, contextMsg)
}
