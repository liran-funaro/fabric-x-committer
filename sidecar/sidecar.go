package sidecar

import (
	"context"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/deliverserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("sidecar")

// Service is a relay service which relays the block from orderer to committer. Further,
// it aggregates the transaction status and forwards the validated block to clients who have
// registered on the ledger server.
type Service struct {
	LedgerService     deliverserver.Server
	coordinatorClient *coordinatorClient
	ordererClient     deliverclient.Client
	aggregator        *Aggregator
}

// New creates a sidecar service.
func New(c *SidecarConfig) (*Service, error) {
	// start ledger service that serves the block deliver api and receives completed blocks from the aggregator
	logger.Infof("Create ledger service at %v\n", c.Server.Endpoint.Address())
	deliverServer := deliverserver.New(c.Orderer.ChannelID, c.Ledger.Path)
	logger.Infof("Created ledger service")

	// start orderer client that forwards blocks to aggregator
	ordererClient, err := deliverclient.New(c.Orderer, &deliverclient.Provider{})
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to orderer on %s", c.Orderer.Endpoint.String())
	}

	// start coordinator client that forwards scBlocks to the coordinator
	// and receives status batches from the coordinator
	coordinatorClient, err := newCoordinatorClient(c.Committer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to coordinator on %s", c.Committer.Endpoint.String())
	}
	logger.Infof("Created coordinator client")

	logger.Infof("Create aggregator")
	agg := NewAggregator(
		func(scBlock *protoblocktx.Block) {
			logger.Debugf("Sending new block to coordinator: [%d:%d]", scBlock.Number, len(scBlock.Txs))
			coordinatorClient.Input() <- scBlock
		},
		func(block *common.Block) {
			logger.Debugf("Adding new block to ledger: [%d:%d]", block.Header.Number, len(block.Data.Data))
			deliverServer.Input() <- block
		},
	)
	logger.Infof("Aggregator created")

	return &Service{
		LedgerService:     deliverServer,
		coordinatorClient: coordinatorClient,
		ordererClient:     ordererClient,
		aggregator:        agg,
	}, nil
}

// Start starts the sidecar service.
func (s *Service) Start(ctx context.Context) (<-chan error, <-chan error, <-chan error, error) { //nolint
	blockChan := make(chan *common.Block, 100)
	statusChan := make(chan *protocoordinatorservice.TxValidationStatusBatch, 100)

	sCtx, cancel := context.WithCancel(ctx)
	utils.RegisterInterrupt(cancel)

	ordererErrChan, err := s.ordererClient.Start(sCtx, blockChan)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to start orderer client")
	}
	logger.Infof("Started listening on orderer")

	coordinatorErrChan, err := s.coordinatorClient.Start(sCtx, statusChan)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to start coordinator client")
	}

	aggErrChan := s.aggregator.Start(sCtx, blockChan, statusChan)

	return ordererErrChan, coordinatorErrChan, aggErrChan, nil
}

// Close closes the sidecar service.
func (s *Service) Close() error {
	logger.Infof("Shutting down sidecar")
	s.aggregator.Close()
	s.ordererClient.Stop()
	return s.coordinatorClient.Close()
}
