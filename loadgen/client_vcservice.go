package loadgen

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type vcClient struct {
	*loadGenClient
	config  *VCClientConfig
	tracker *vcTracker
}

func newVCClient(config *VCClientConfig, metrics *PerfMetrics) *vcClient {
	return &vcClient{
		loadGenClient: newLoadGenClient(),
		tracker:       newVCTracker(metrics),
		config:        config,
	}
}

func (c *vcClient) startWorkload(blockGen *BlockStream) error {
	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Wrap(err, "failed opening connection to vc-service")
	}

	for _, conn := range connections {
		stream, err := c.openVCStream(conn)
		if err != nil {
			return errors.Wrapf(err, "failed opening stream to %s", conn.Target())
		}
		go c.startSending(blockGen, stream)
		go c.startReceiving(stream)
	}

	return nil
}

func (c *vcClient) startNamespaceGeneration(nsGen *NamespaceGenerator) error {
	conn, err := connection.Connect(
		connection.NewDialConfigWithCreds(*c.config.Endpoints[0], insecure.NewCredentials()),
	)
	utils.Must(err)

	stream, err := c.openVCStream(conn)
	utils.Must(err)

	blkToSend := nsGen.Next()
	logger.Debugf("Sending block %d with %d TXs", blkToSend.Number, len(blkToSend.Txs))
	utils.Must(stream.Send(mapVCBatch(blkToSend)))
	c.tracker.OnSendBlock(blkToSend)

	txStatus, err := stream.Recv()
	utils.Must(err)
	if !c.tracker.OnReceiveVCBatch(txStatus) {
		c.cancel(errors.New("could not commit namespace generation-block"))
	}

	return conn.Close()
}

func (c *vcClient) Start(blockGen *BlockStream, nsGen *NamespaceGenerator) error {
	if err := c.startNamespaceGeneration(nsGen); err != nil {
		return errors.Wrap(err, "failed closing namespace generation connection")
	}

	return c.startWorkload(blockGen)
}

func (c *vcClient) startReceiving(
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) {
	for c.ctx.Err() == nil {
		if response, err := stream.Recv(); err != nil {
			c.cancel(connection.WrapStreamRpcError(err))
		} else {
			logger.Debugf("Received batch with %d responses", len(response.Status))
			c.tracker.OnReceiveVCBatch(response)
		}
	}
}

func (c *vcClient) startSending(
	blockGen *BlockStream,
	stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) {
	c.loadGenClient.startSending(blockGen, stream, func(block *protoblocktx.Block) error {
		logger.Debugf("Sending block %d with %d TXs", block.Number, len(block.Txs))
		err := stream.Send(mapVCBatch(block))
		c.tracker.OnSendBlock(block)
		return err
	})
}

func mapVCBatch(block *protoblocktx.Block) *protovcservice.TransactionBatch {
	txBatch := &protovcservice.TransactionBatch{}
	for txNum, tx := range block.Txs {
		txBatch.Transactions = append(
			txBatch.Transactions,
			&protovcservice.Transaction{
				ID:          tx.Id,
				Namespaces:  tx.Namespaces,
				BlockNumber: block.Number,
				TxNum:       uint32(txNum), //nolint:gosec
			},
		)
	}
	return txBatch
}

func (c *vcClient) openVCStream( //nolint:ireturn
	conn *grpc.ClientConn,
) (protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient, error) {
	logger.Infof("Creating client to %s", conn.Target())
	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	logger.Info("VC Client created")

	logger.Info("Opening VC stream")
	return client.StartValidateAndCommitStream(c.ctx)
}

type vcTracker struct {
	tracker.ReceiverSender
}

func newVCTracker(metrics *PerfMetrics) *vcTracker {
	return &vcTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *vcTracker) OnReceiveVCBatch(batch *protovcservice.TransactionStatus) bool {
	logger.Debugf("Received VC batch with %d items", len(batch.Status))

	committed := 0
	for id, status := range batch.Status {
		txCommitStatus := status == protoblocktx.Status_COMMITTED
		if txCommitStatus {
			committed++
		}
		t.OnReceiveTransaction(id, txCommitStatus)
	}
	return committed == len(batch.Status)
}
