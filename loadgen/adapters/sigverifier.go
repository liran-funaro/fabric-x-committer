package adapters

import (
	"context"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/credentials/insecure"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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
	policyMsg, err := workload.CreatePolicies(c.res.Profile.Transaction.Policy)
	if err != nil {
		return errors.Wrap(err, "failed creating verification policy")
	}

	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Newf("failed opening connections: %w", err)
	}
	defer connection.CloseConnectionsLog(connections...)

	g, gCtx := errgroup.WithContext(ctx)
	streams := make([]protosigverifierservice.Verifier_StartStreamClient, len(connections))
	for i, conn := range connections {
		client := protosigverifierservice.NewVerifierClient(conn)

		logger.Infof("Set verification verification policy")
		if _, err = client.UpdatePolicies(ctx, policyMsg); err != nil {
			return errors.Wrap(err, "failed setting verification policy")
		}

		logger.Infof("Opening stream to %s", c.config.Endpoints[i])
		streams[i], err = client.StartStream(gCtx)
		if err != nil {
			return errors.Wrapf(err, "failed opening a stream to %s", c.config.Endpoints[i])
		}
	}

	for _, stream := range streams {
		g.Go(func() error {
			return c.sendBlocks(gCtx, txStream, func(block *protoblocktx.Block) error {
				return stream.Send(mapVSBatch(block))
			})
		})
		g.Go(func() error {
			return c.receiveStatus(gCtx, stream)
		})
	}
	return g.Wait()
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
			logger.Infof("Received response: %s", response.Status)
			c.res.Metrics.OnReceiveTransaction(response.TxId, response.Status)
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
