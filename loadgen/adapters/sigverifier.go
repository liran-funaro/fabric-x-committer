package adapters

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/credentials/insecure"
)

type (
	// SvAdapter applies load on the SV.
	SvAdapter struct {
		commonAdapter
		config *SVClientConfig
	}
)

// NewSVAdapter instantiate SvAdapter.
func NewSVAdapter(config *SVClientConfig, res *ClientResources) *SvAdapter {
	return &SvAdapter{
		commonAdapter: commonAdapter{res: res},
		config:        config,
	}
}

// RunWorkload applies load on the SV.
func (c *SvAdapter) RunWorkload(ctx context.Context, txStream TxStream) error {
	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Wrap(err, "failed opening connections")
	}
	defer connection.CloseConnectionsLog(connections...)

	streams := make([]protosigverifierservice.Verifier_StartStreamClient, 0, len(connections))
	for _, conn := range connections {
		client := protosigverifierservice.NewVerifierClient(conn)

		logger.Infof("Set verification verification keys")
		for _, key := range getKeys(c.res) {
			_, err = client.SetVerificationKey(ctx, key)
			if err != nil {
				return errors.Wrap(err, "failed setting verification key")
			}
		}

		logger.Infof("Opening stream")
		stream, err := client.StartStream(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed opening connection to %s", conn.Target())
		}
		streams = append(streams, stream)
	}

	g, gCtx := errgroup.WithContext(ctx)
	for _, stream := range streams {
		g.Go(func() error {
			return c.sendBlocks(ctx, txStream, func(block *protoblocktx.Block) error {
				return stream.Send(mapVSBatch(block))
			})
		})
		g.Go(func() error {
			return c.receiveStatus(gCtx, stream)
		})
	}
	return g.Wait()
}

func getKeys(res *ClientResources) []*protosigverifierservice.Key {
	e := workload.NewTxSignerVerifier(res.Profile.Transaction.Policy)
	keys := make([]*protosigverifierservice.Key, 0, len(e.HashSigners))
	for nsID, s := range e.HashSigners {
		p := s.GetVerificationPolicy()
		keys = append(keys, &protosigverifierservice.Key{
			NsId:            uint32(nsID),
			SerializedBytes: p.PublicKey,
			Scheme:          p.Scheme,
		})
	}
	return keys
}

func (c *SvAdapter) receiveStatus(
	ctx context.Context, stream protosigverifierservice.Verifier_StartStreamClient,
) error {
	for ctx.Err() == nil {
		responseBatch, err := stream.Recv()
		if err != nil {
			return connection.FilterStreamRPCError(err)
		}

		logger.Debugf("Received SV batch with %d responses", len(responseBatch.Responses))
		for _, response := range responseBatch.Responses {
			status := protoblocktx.Status_COMMITTED
			if !response.IsValid {
				status = protoblocktx.Status_ABORTED_SIGNATURE_INVALID
			}
			c.res.Metrics.OnReceiveTransaction(response.TxId, status)
		}
	}
	return nil
}

func mapVSBatch(b *protoblocktx.Block) *protosigverifierservice.RequestBatch {
	reqs := make([]*protosigverifierservice.Request, len(b.Txs))
	for i, tx := range b.Txs {
		reqs[i] = &protosigverifierservice.Request{
			BlockNum: b.Number,
			TxNum:    uint64(b.TxsNum[i]),
			Tx:       tx,
		}
	}
	return &protosigverifierservice.RequestBatch{Requests: reqs}
}
