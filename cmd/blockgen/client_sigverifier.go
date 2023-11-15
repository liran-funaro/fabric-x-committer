package main

import (
	"context"

	"github.com/pkg/errors"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type svClient struct {
	*loadGenClient
	config *SVClientConfig
}

func newSVClient(config *SVClientConfig, tracker *ClientTracker, logger CmdLogger) blockGenClient {
	return &svClient{
		loadGenClient: &loadGenClient{
			tracker:    tracker,
			logger:     logger,
			stopSender: make(chan any, len(config.Endpoints)),
		},
		config: config,
	}
}

func (c *svClient) Start(blockGen *loadgen.BlockStreamGenerator) error {
	connections := make([]*connection.DialConfig, len(c.config.Endpoints))
	for i, endpoint := range c.config.Endpoints {
		connections[i] = connection.NewDialConfig(*endpoint)
	}

	requests := make(chan *sigverification.RequestBatch, len(c.config.Endpoints))
	for i, conn := range connections {
		stream, err := c.startStream(conn, blockGen.Signer.GetVerificationKey())
		if err != nil {
			return errors.Wrapf(err, "failed opening connection to %s", c.config.Endpoints[i].String())
		}
		go func(i int) {
			for {
				resp, err := stream.Recv()
				if err != nil {
					panic(errors.Wrapf(err, "failed receiving from endpoint %s", c.config.Endpoints[i].String()))
				}
				c.logger("got %d responses", len(resp.GetResponses()))
				c.tracker.OnReceiveSVBatch(resp)
			}
		}(i)
		go func(i int) {
			for {
				batch := <-requests
				if err := stream.Send(batch); err != nil {
					panic(errors.Wrapf(err, "failed sending to endpoint %s", c.config.Endpoints[i].String()))
				}
				c.tracker.OnSendSVBatch(batch)
			}
		}(i)
	}

	for {
		b := <-blockGen.BlockQueue
		reqs := make([]*sigverification.Request, len(b.Txs))
		for i, tx := range b.Txs {
			reqs[i] = &sigverification.Request{
				BlockNum: b.Number,
				TxNum:    uint64(i),
				Tx:       tx,
			}
		}
		requests <- &sigverification.RequestBatch{Requests: reqs}
	}
}

func (c *svClient) startStream(conn *connection.DialConfig, verificationKey signature.PublicKey) (sigverification.Verifier_StartStreamClient, error) {
	clientConnection, err := connection.Connect(conn)
	if err != nil {
		return nil, errors.Wrap(err, "failed connecting to server")
	}
	c.logger("Connected to server %s", conn.Address())
	client := sigverification.NewVerifierClient(clientConnection)
	c.logger("Created verifier client")

	_, err = client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	if err != nil {
		return nil, errors.Wrap(err, "failed setting verification key")
	}
	c.logger("Set verification verificationKey")

	stream, err := client.StartStream(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "failed starting stream")
	}
	c.logger("Started stream to %s", conn.Address())
	return stream, nil
}
