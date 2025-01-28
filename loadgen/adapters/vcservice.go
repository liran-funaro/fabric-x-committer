package adapters

import (
	"context"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/credentials/insecure"
)

type (
	// VcAdapter applies load on the VC.
	VcAdapter struct {
		commonAdapter
		config *VCClientConfig
	}
)

// NewVCAdapter instantiate VcAdapter.
func NewVCAdapter(config *VCClientConfig, res *ClientResources) *VcAdapter {
	return &VcAdapter{
		commonAdapter: commonAdapter{res: res},
		config:        config,
	}
}

// RunWorkload applies load on the VC.
func (c *VcAdapter) RunWorkload(ctx context.Context, txStream TxStream) error {
	connections, err := connection.OpenConnections(c.config.Endpoints, insecure.NewCredentials())
	if err != nil {
		return errors.Wrap(err, "failed opening connection to vc-service")
	}
	defer connection.CloseConnectionsLog(connections...)

	streams := make([]protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient, 0, len(connections))
	for i, conn := range connections {
		client := protovcservice.NewValidationAndCommitServiceClient(conn)
		if i == 0 {
			if lastBlockNum, err := client.GetLastCommittedBlockNumber(ctx, nil); err != nil {
				// We do not return error as we can proceed assuming no blocks were committed.
				logger.Infof("failed getting last committed block number: %v", err)
			} else {
				c.nextBlockNum.Store(lastBlockNum.Number + 1)
			}
		}

		logger.Info("Opening VC stream")
		stream, err := client.StartValidateAndCommitStream(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed opening stream to %s", conn.Target())
		}
		streams = append(streams, stream)
	}

	g, gCtx := errgroup.WithContext(ctx)
	for _, stream := range streams {
		g.Go(func() error {
			return c.sendBlocks(ctx, txStream, func(block *protoblocktx.Block) error {
				return stream.Send(mapVCBatch(block))
			})
		})
		g.Go(func() error {
			return c.receiveStatus(gCtx, stream)
		})
	}
	return g.Wait()
}

func (c *VcAdapter) receiveStatus(
	ctx context.Context, stream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) error {
	for ctx.Err() == nil {
		responseBatch, err := stream.Recv()
		if err != nil {
			return connection.WrapStreamRpcError(err)
		}

		logger.Debugf("Received VC batch with %d items", len(responseBatch.Status))
		for id, status := range responseBatch.Status {
			c.res.Metrics.OnReceiveTransaction(id, status.Code)
		}
	}
	return nil
}

func mapVCBatch(block *protoblocktx.Block) *protovcservice.TransactionBatch {
	txBatch := &protovcservice.TransactionBatch{}
	for i, tx := range block.Txs {
		txBatch.Transactions = append(
			txBatch.Transactions,
			&protovcservice.Transaction{
				ID:          tx.Id,
				Namespaces:  tx.Namespaces,
				BlockNumber: block.Number,
				TxNum:       block.TxsNum[i],
			},
		)
	}
	return txBatch
}
