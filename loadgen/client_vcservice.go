package loadgen

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type vcClient struct {
	*loadGenClient
	config  *VCClientConfig
	tracker *vcTracker
}

func newVCClient(config *VCClientConfig, metrics *PerfMetrics) blockGenClient { //nolint:ireturn
	return &vcClient{
		loadGenClient: &loadGenClient{
			stopSender: make(chan any, len(config.Endpoints)),
		},
		tracker: newVCTracker(metrics),
		config:  config,
	}
}

func (c *vcClient) Start(blockGen *BlockStreamGenerator) error {
	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Wrap(err, "failed opening connections")
	}

	errChan := make(chan error, len(connections))
	for _, conn := range connections {
		stream, err := openVCStream(conn)
		if err != nil {
			return errors.Wrapf(err, "failed opening stream to %s", conn.Target())
		}

		go func() {
			errChan <- c.startSending(blockGen, stream)
		}()

		go func() {
			errChan <- c.startReceiving(stream)
		}()
	}

	return <-errChan
}

func (c *vcClient) startReceiving(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) error {
	for {
		if response, err := stream.Recv(); err != nil {
			return connection.WrapStreamRpcError(err)
		} else {
			logger.Debugf("Received batch with %d responses", len(response.Status))
			c.tracker.OnReceiveVCBatch(response)
		}
	}
}

func (c *vcClient) startSending(
	blockGen *BlockStreamGenerator,
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) error {
	return c.loadGenClient.startSending(blockGen.BlockQueue, stream, func(block *protoblocktx.Block) error {
		logger.Debugf("Sending block %d with %d TXs", block.Number, len(block.Txs))
		err := stream.Send(mapVCBatch(block))
		c.tracker.OnSendBlock(block)
		return err
	})
}

func mapVCBatch(block *protoblocktx.Block) *protovcservice.TransactionBatch {
	txBatch := &protovcservice.TransactionBatch{}
	for _, tx := range block.Txs {
		txBatch.Transactions = append(
			txBatch.Transactions,
			&protovcservice.Transaction{
				ID:         tx.Id,
				Namespaces: tx.Namespaces,
			},
		)
	}
	return txBatch
}

func openVCStream( //nolint:ireturn
	conn *grpc.ClientConn,
) (protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient, error) {
	logger.Infof("Creating client to %s", conn.Target())
	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	logger.Info("VC Client created")

	logger.Info("Opening VC stream")
	return client.StartValidateAndCommitStream(context.Background())
}

type vcTracker struct {
	tracker.ReceiverSender
}

func newVCTracker(metrics *PerfMetrics) *vcTracker {
	return &vcTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *vcTracker) OnReceiveVCBatch(batch *protovcservice.TransactionStatus) {
	logger.Debugf("Received VC batch with %d items", len(batch.Status))

	for id, status := range batch.Status {
		t.OnReceiveTransaction(id, status == protoblocktx.Status_COMMITTED)
	}
}
