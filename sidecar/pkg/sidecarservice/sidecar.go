package sidecarservice

import (
	"context"
	errors2 "errors"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/aggregator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/coordinator"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/ledger"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar/pkg/orderer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("sidecar service")

type ServiceImpl struct {
	*aggregator.Aggregator
	*coordinator.CoordinatorClient
	*orderer.OrdererClient
	*ledger.LedgerDeliverServer
}

func (s *ServiceImpl) Close() error {
	logger.Infof("Shutting down sidecar")
	s.Aggregator.Close()
	return errors2.Join([]error{s.CoordinatorClient.Close(),
		s.OrdererClient.Close(),
	}...)
}

func NewService(c *SidecarConfig) (*ServiceImpl, error) {

	// start ledger service
	// that serves the block deliver api and receives completed blocks from the aggregator
	logger.Infof("Create ledger service at %v\n", c.Server.Endpoint.Address())
	ledgerService := ledger.NewLedgerDeliverServer(c.Orderer.ChannelID, c.Ledger.Path)
	logger.Infof("Created ledger service")

	// start orderer client that forwards blocks to aggregator
	logger.Infof("Create orderer client and connect to %v\n", c.Orderer.Endpoint)
	creds, signer := connection.GetOrdererConnectionCreds(c.Orderer.OrdererConnectionProfile)
	ordererConn, err := connection.Connect(connection.NewDialConfigWithCreds(c.Orderer.Endpoint, creds))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to orderer on %s", c.Orderer.Endpoint.String())
	}
	ordererClient := orderer.NewOrdererClient(ordererConn, signer, c.Orderer.ChannelID, int64(0))
	logger.Infof("Started orderer client on channel %s", c.Orderer.ChannelID)

	// start coordinator client
	// that forwards scBlocks to the coordinator and receives status batches from the coordinator
	logger.Infof("Create coordinator client and connect to %v\n", c.Committer.Endpoint)
	coordinatorConn, err := connection.Connect(connection.NewDialConfig(c.Committer.Endpoint))
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to coordinator")
	}
	coordinatorClient := coordinator.NewCoordinatorClient(coordinatorConn)
	logger.Infof("Created coordinator client")

	logger.Infof("Create aggregator")
	agg := aggregator.New(
		func(scBlock *protoblocktx.Block) {
			logger.Debugf("Sending new block to coordinator: [%d:%d]", scBlock.Number, len(scBlock.Txs))
			coordinatorClient.Input() <- scBlock
		},
		func(block *common.Block) {
			logger.Debugf("Adding new block to ledger: [%d:%d]", block.Header.Number, len(block.Data.Data))
			ledgerService.Input() <- block
		},
	)
	logger.Infof("Aggregator created")

	return &ServiceImpl{
		Aggregator:          agg,
		CoordinatorClient:   coordinatorClient,
		OrdererClient:       ordererClient,
		LedgerDeliverServer: ledgerService,
	}, nil
}

func (s *ServiceImpl) Start(ctx context.Context) (<-chan error, <-chan error, <-chan error, error) {
	blockChan := make(chan *common.Block, 100)
	statusChan := make(chan *protocoordinatorservice.TxValidationStatusBatch, 100)

	sCtx, cancel := context.WithCancel(ctx)
	utils.RegisterInterrupt(cancel)

	ordererErrChan, err := s.OrdererClient.Start(sCtx, blockChan)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to start orderer client")
	}
	logger.Infof("Started listening on orderer")

	coordinatorErrChan, err := s.CoordinatorClient.Start(sCtx, statusChan)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed to start coordinator client")
	}

	aggErrChan := s.Aggregator.Start(sCtx, blockChan, statusChan)

	return ordererErrChan, coordinatorErrChan, aggErrChan, nil
}
