package loadgen

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type coordinatorClient struct {
	*loadGenClient
	config  *CoordinatorClientConfig
	tracker *coordinatorTracker
}

func newCoordinatorClient(
	config *CoordinatorClientConfig, metrics *PerfMetrics,
) *coordinatorClient {
	return &coordinatorClient{
		loadGenClient: newLoadGenClient(),
		tracker:       newCoordinatorTracker(metrics),
		config:        config,
	}
}

func (c *coordinatorClient) startWorkload(blockGen *BlockStream) error {
	conn, err := c.openConnection()
	if err != nil {
		return err
	}
	verificationKey, keyScheme := blockGen.Signer.HashSigner.GetVerificationKey()
	stream, err := c.openCoordinatorStream(conn, verificationKey, keyScheme)
	if err != nil {
		return errors.Wrap(err, "failed creating stream to coordinator")
	}

	go c.loadGenClient.startSending(blockGen, stream, func(block *protoblocktx.Block) error {
		err := stream.Send(block)
		c.tracker.OnSendBlock(block)
		return err
	})
	go c.startReceiving(stream)

	return nil
}

func (c *coordinatorClient) startNamespaceGeneration(nsGen *NamespaceGenerator) error {
	conn, err := c.openConnection()
	utils.Must(err)

	stream, err := c.openCoordinatorStream(conn, nsGen.getSigner().GetVerificationKey(), nsGen.scheme)
	utils.Must(err)

	blkToSend := nsGen.Next()

	utils.Must(stream.Send(blkToSend))
	c.tracker.OnSendBlock(blkToSend)

	txStatus, err := stream.Recv()
	utils.Must(err)

	if !c.tracker.OnReceiveCoordinatorBatch(txStatus) {
		c.cancel(errors.New("could not commit namespace generation-block"))
	}

	return conn.Close()
}

func (c *coordinatorClient) Start(blockGen *BlockStream, nsGen *NamespaceGenerator) error {
	if err := c.startNamespaceGeneration(nsGen); err != nil {
		return errors.Wrap(err, "failed closing namespace generation connection")
	}

	return c.startWorkload(blockGen)
}

func (c *coordinatorClient) startReceiving(stream protocoordinatorservice.Coordinator_BlockProcessingClient) {
	for c.ctx.Err() == nil {
		if txStatus, err := stream.Recv(); err != nil {
			c.cancel(errors.Wrap(err, "failed receiving tx"))
		} else {
			c.tracker.OnReceiveCoordinatorBatch(txStatus)
		}
	}
}

func (c *coordinatorClient) openConnection() (*grpc.ClientConn, error) {
	conn, err := connection.Connect(connection.NewDialConfig(*c.config.Endpoint))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to %s", c.config.Endpoint.String())
	}
	logger.Info("Connected to coordinator")
	return conn, nil
}

type coordinatorTracker struct {
	tracker.ReceiverSender
}

func newCoordinatorTracker(metrics *PerfMetrics) *coordinatorTracker {
	return &coordinatorTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *coordinatorTracker) OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch) bool {
	logger.Debugf("Received coordinator batch with %d items", len(batch.TxsValidationStatus))

	committed := 0
	for _, tx := range batch.TxsValidationStatus {
		txCommitStatus := tx.Status == protoblocktx.Status_COMMITTED
		if txCommitStatus {
			committed++
		}
		t.OnReceiveTransaction(tx.TxId, txCommitStatus)
	}
	return committed == len(batch.TxsValidationStatus)
}

func (c *coordinatorClient) openCoordinatorStream( //nolint:ireturn
	conn *grpc.ClientConn, publicKey signature.PublicKey, keyScheme signature.Scheme,
) (protocoordinatorservice.Coordinator_BlockProcessingClient, error) {
	client := openCoordinatorClient(conn)

	_, err := client.SetMetaNamespaceVerificationKey(
		c.ctx,
		&protosigverifierservice.Key{
			NsId:            uint32(types.MetaNamespaceID),
			NsVersion:       types.VersionNumber(0).Bytes(),
			SerializedBytes: publicKey,
			Scheme:          keyScheme,
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	logger.Info("Verification key set")

	logger.Info("Opening stream")
	return client.BlockProcessing(c.ctx)
}

func openCoordinatorClient(conn *grpc.ClientConn) protocoordinatorservice.CoordinatorClient {
	logger.Infof("Opening client to %s", conn.Target())
	return protocoordinatorservice.NewCoordinatorClient(conn)
}
