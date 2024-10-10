package loadgen

import (
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/tracker"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type svClient struct {
	*loadGenClient
	config  *SVClientConfig
	tracker *svTracker
}

func newSVClient(config *SVClientConfig, metrics *PerfMetrics) *svClient {
	return &svClient{
		loadGenClient: newLoadGenClient(),
		tracker:       newSVTracker(metrics),
		config:        config,
	}
}

func (c *svClient) startWorkload(blockGen *BlockStream) error {
	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Wrap(err, "failed opening connections")
	}

	for _, conn := range connections {
		stream, err := c.openVSStream(
			conn,
			blockGen.Signer.GetVerificationKey(),
			blockGen.Signer.HashSigner.scheme,
			blockGen.NsID,
		)
		if err != nil {
			return errors.Wrapf(err, "failed opening connection to %s", conn.Target())
		}

		go c.startSending(blockGen, stream)
		go c.startReceiving(stream)
	}

	return nil
}

func (c *svClient) startNamespaceGeneration(nsGen *NamespaceGenerator) error {
	conn, err := connection.Connect(
		connection.NewDialConfigWithCreds(*c.config.Endpoints[0], insecure.NewCredentials()),
	)
	utils.Must(err)
	stream, err := c.openVSStream(
		conn,
		nsGen.getSigner().GetVerificationKey(),
		nsGen.scheme,
		uint32(types.MetaNamespaceID),
	)
	utils.Must(err)

	blkToSend := nsGen.Next()

	logger.Debugf("Sending block %d", blkToSend.Number)
	utils.Must(stream.Send(mapVSBatch(blkToSend)))
	c.tracker.OnSendBlock(blkToSend)

	txStatus, err := stream.Recv()
	utils.Must(err)
	if !c.tracker.OnReceiveSVBatch(txStatus) {
		logger.Infof("could not validate namespace-generation-block transaction")
	}

	return conn.Close()
}

func (c *svClient) Start(blockGen *BlockStream, nsGen *NamespaceGenerator) error {
	if err := c.startNamespaceGeneration(nsGen); err != nil {
		return errors.Wrap(err, "failed closing namespace generation connection")
	}

	return c.startWorkload(blockGen)
}

func (c *svClient) startReceiving(stream sigverification.Verifier_StartStreamClient) {
	for c.ctx.Err() == nil {
		if response, err := stream.Recv(); err != nil {
			c.cancel(errors.Wrapf(err, "failed receiving"))
		} else {
			logger.Debugf("Got %d responses", len(response.GetResponses()))
			c.tracker.OnReceiveSVBatch(response)
		}
	}
}

func (c *svClient) startSending(
	blockGen *BlockStream, stream sigverification.Verifier_StartStreamClient,
) {
	c.loadGenClient.startSending(blockGen, stream, func(block *protoblocktx.Block) error {
		logger.Debugf("Sending block %d", block.Number)
		err := stream.Send(mapVSBatch(block))
		c.tracker.OnSendBlock(block)
		return err
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

func (c *svClient) openVSStream( //nolint:ireturn
	conn *grpc.ClientConn, verificationKey signature.PublicKey, scheme Scheme, nsID uint32,
) (sigverification.Verifier_StartStreamClient, error) {
	logger.Infof("Creating client to %s\n", conn.Target())
	client := sigverification.NewVerifierClient(conn)
	logger.Infof("Created verifier client")

	k := &sigverification.Key{
		NsId:            nsID,
		NsVersion:       types.VersionNumber(0).Bytes(),
		SerializedBytes: verificationKey,
		Scheme:          scheme,
	}

	_, err := client.SetVerificationKey(c.ctx, k)
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	logger.Infof("Set verification verificationKey")

	logger.Infof("Opening stream")
	return client.StartStream(c.ctx)
}

type svTracker struct {
	tracker.ReceiverSender
}

func newSVTracker(metrics *PerfMetrics) *svTracker {
	return &svTracker{ReceiverSender: NewClientTracker(metrics)}
}

func (t *svTracker) OnReceiveSVBatch(batch *sigverification.ResponseBatch) bool {
	logger.Debugf("Received batch with %d responses", len(batch.Responses))

	validResponses := 0
	for _, response := range batch.Responses {
		if response.IsValid {
			validResponses++
		}
		t.OnReceiveTransaction(response.TxId, response.IsValid)
	}
	return validResponses == len(batch.Responses)
}
