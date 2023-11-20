package loadgen

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type svClient struct {
	*loadGenClient
	config  *SVClientConfig
	tracker *svTracker
}

func newSVClient(config *SVClientConfig, metrics *perfMetrics) blockGenClient {
	tracker := newSVTracker(metrics)
	return &svClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			stopSender: make(chan any, len(config.Endpoints)),
		},
		tracker: tracker,
		config:  config,
	}
}

func (c *svClient) Start(blockGen *BlockStreamGenerator) error {
	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Wrap(err, "failed opening connections")
	}

	errChan := make(chan error, len(connections))
	for _, conn := range connections {
		stream, err := openVSStream(conn, blockGen.Signer.GetVerificationKey())
		if err != nil {
			return errors.Wrapf(err, "failed opening connection to %s", conn.Target())
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

func (c *svClient) startReceiving(stream sigverification.Verifier_StartStreamClient) error {
	for {
		if response, err := stream.Recv(); err != nil {
			return errors.Wrapf(err, "failed receiving")
		} else {
			logger.Debugf("Got %d responses", len(response.GetResponses()))
			c.tracker.OnReceiveSVBatch(response)
		}
	}
}

func (c *svClient) startSending(blockGen *BlockStreamGenerator, stream sigverification.Verifier_StartStreamClient) error {
	return c.loadGenClient.startSending(blockGen.BlockQueue, stream, func(block *protoblocktx.Block) error {
		logger.Debugf("Sending block %d", block.Number)
		return stream.Send(mapVSBatch(block))
	})
}

func mapVSBatch(b *protoblocktx.Block) *sigverification.RequestBatch {
	reqs := make([]*sigverification.Request, len(b.Txs))
	for i, tx := range b.Txs {
		reqs[i] = &sigverification.Request{
			BlockNum: b.Number,
			TxNum:    uint64(i),
			Tx:       tx,
		}
	}
	batch := &sigverification.RequestBatch{Requests: reqs}
	return batch
}

func openVSStream(conn *grpc.ClientConn, verificationKey signature.PublicKey) (sigverification.Verifier_StartStreamClient, error) {
	logger.Infof("Creating client to %s\n", conn.Target())
	client := sigverification.NewVerifierClient(conn)
	logger.Infof("Created verifier client")

	_, err := client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	logger.Infof("Set verification verificationKey")

	logger.Infof("Opening stream")
	return client.StartStream(context.Background())
}

type svTracker struct {
	tracker.ReceiverSender
}

func newSVTracker(metrics *perfMetrics) *svTracker {
	return &svTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *svTracker) OnReceiveSVBatch(batch *sigverification.ResponseBatch) {
	logger.Debugf("Received batch with %d responses", len(batch.Responses))

	for _, response := range batch.Responses {
		t.OnReceiveTransaction(response.TxId, response.IsValid)
	}
}
