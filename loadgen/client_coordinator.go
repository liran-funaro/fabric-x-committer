package loadgen

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type coordinatorClient struct {
	*loadGenClient
	config  *CoordinatorClientConfig
	tracker *coordinatorTracker
}

func newCoordinatorClient( //nolint:ireturn
	config *CoordinatorClientConfig, metrics *PerfMetrics,
) blockGenClient {
	return &coordinatorClient{
		loadGenClient: &loadGenClient{
			stopSender: make(chan any),
		},
		tracker: newCoordinatorTracker(metrics),
		config:  config,
	}
}

func (c *coordinatorClient) Start(blockGen *BlockStreamGenerator) error {
	conn, err := connection.Connect(connection.NewDialConfig(*c.config.Endpoint))
	if err != nil {
		return errors.Wrapf(err, "failed to connect to %s", c.config.Endpoint.String())
	}
	logger.Info("Connected to coordinator")

	verificationKey, keyScheme := blockGen.Signer.HashSigner.GetVerificationKey()
	stream, err := openCoordinatorStream(conn, verificationKey, keyScheme)
	if err != nil {
		return errors.Wrap(err, "failed creating stream to coordinator")
	}

	errChan := make(chan error)
	go func() {
		errChan <- c.loadGenClient.startSending(
			blockGen.BlockQueue, stream, func(block *protoblocktx.Block) error {
				err := stream.Send(block)
				c.tracker.OnSendBlock(block)
				return err
			})
	}()
	go func() {
		errChan <- c.startReceiving(stream)
	}()

	return <-errChan
}

func (c *coordinatorClient) startReceiving(stream protocoordinatorservice.Coordinator_BlockProcessingClient) error {
	for {
		if txStatus, err := stream.Recv(); err != nil {
			return errors.Wrap(err, "failed receiving tx")
		} else {
			c.tracker.OnReceiveCoordinatorBatch(txStatus)
		}
	}
}

type coordinatorTracker struct {
	tracker.ReceiverSender
}

func newCoordinatorTracker(metrics *PerfMetrics) *coordinatorTracker {
	return &coordinatorTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *coordinatorTracker) OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch) {
	logger.Debugf("Received coordinator batch with %d items", len(batch.TxsValidationStatus))

	for _, tx := range batch.TxsValidationStatus {
		t.OnReceiveTransaction(tx.TxId, tx.Status == protoblocktx.Status_COMMITTED)
	}
}

func openCoordinatorStream( //nolint:ireturn
	conn *grpc.ClientConn, publicKey signature.PublicKey, keyScheme signature.Scheme,
) (protocoordinatorservice.Coordinator_BlockProcessingClient, error) {
	client := openCoordinatorClient(conn)

	_, err := client.SetMetaNamespaceVerificationKey(
		context.Background(),
		&protosigverifierservice.Key{
			NsId:            uint32(types.MetaNamespaceID),
			SerializedBytes: publicKey,
			Scheme:          keyScheme,
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	logger.Info("Verification key set")

	logger.Info("Opening stream")
	return client.BlockProcessing(context.Background())
}

func openCoordinatorClient(conn *grpc.ClientConn) protocoordinatorservice.CoordinatorClient {
	logger.Infof("Opening client to %s", conn.Target())
	return protocoordinatorservice.NewCoordinatorClient(conn)
}
