package loadgen

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
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

func newCoordinatorClient(config *CoordinatorClientConfig, metrics *perfMetrics) blockGenClient {
	tracker := newCoordinatorTracker(metrics)
	return &coordinatorClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			stopSender: make(chan any),
		},
		tracker: tracker,
		config:  config,
	}
}

func (c *coordinatorClient) Start(blockGen *BlockStreamGenerator) error {
	conn, err := connection.Connect(connection.NewDialConfig(*c.config.Endpoint))
	if err != nil {
		return errors.Wrapf(err, "failed to connect to %s", c.config.Endpoint.String())
	}
	logger.Info("Connected to coordinator")

	stream, err := openCoordinatorStream(conn, blockGen.Signer.GetVerificationKey())
	if err != nil {
		return errors.Wrap(err, "failed creating stream to coordinator")
	}

	errChan := make(chan error)
	go func() {
		errChan <- c.loadGenClient.startSending(blockGen.BlockQueue, stream, stream.Send)
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

func newCoordinatorTracker(metrics *perfMetrics) *coordinatorTracker {
	return &coordinatorTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *coordinatorTracker) OnReceiveCoordinatorBatch(batch *protocoordinatorservice.TxValidationStatusBatch) {
	logger.Debugf("Received coordinator batch with %d items", len(batch.TxsValidationStatus))

	for _, tx := range batch.TxsValidationStatus {
		t.OnReceiveTransaction(tx.TxId, tx.Status == protoblocktx.Status_COMMITTED)
	}
}

func openCoordinatorStream(conn *grpc.ClientConn, publicKey signature.PublicKey) (protocoordinatorservice.Coordinator_BlockProcessingClient, error) {
	client := openCoordinatorClient(conn)

	_, err := client.SetVerificationKey(context.Background(), &protosigverifierservice.Key{SerializedBytes: publicKey})
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
