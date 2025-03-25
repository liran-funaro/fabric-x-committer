package adapters

import (
	"context"
	"fmt"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/sidecar/sidecarclient"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type receiverConfig struct {
	Endpoint  *connection.Endpoint
	ChannelID string
	Res       *ClientResources
}

const committedBlocksQueueSize = 1024

// runReceiver start receiving blocks from the sidecar.
func runReceiver(ctx context.Context, config *receiverConfig) error {
	ledgerReceiver, err := sidecarclient.New(&sidecarclient.Config{
		ChannelID: config.ChannelID,
		Endpoint:  config.Endpoint,
	})
	if err != nil {
		return errors.Wrap(err, "failed to create ledger receiver")
	}

	g, gCtx := errgroup.WithContext(ctx)
	committedBlock := make(chan *common.Block, committedBlocksQueueSize)
	g.Go(func() error {
		return ledgerReceiver.Deliver(gCtx, &sidecarclient.DeliverConfig{
			EndBlkNum:   broadcastdeliver.MaxBlockNum,
			OutputBlock: committedBlock,
		})
	})
	g.Go(func() error {
		receiveCommittedBlock(gCtx, committedBlock, config.Res)
		return context.Canceled
	})
	return errors.Wrap(g.Wait(), "failed running sidecar receiver")
}

func receiveCommittedBlock(
	ctx context.Context,
	blockQueue <-chan *common.Block,
	res *ClientResources,
) {
	committedBlock := channel.NewReader(ctx, blockQueue)
	for ctx.Err() == nil {
		block, ok := committedBlock.Read()
		if !ok {
			return
		}

		statusCodes := block.Metadata.Metadata[common.BlockMetadataIndex_TRANSACTIONS_FILTER]
		logger.Infof("Received block #%d with %d TXs and %d statuses [%s]",
			block.Header.Number, len(block.Data.Data), len(statusCodes), recapStatusCodes(statusCodes),
		)

		for i, data := range block.Data.Data {
			_, channelHeader, err := serialization.UnwrapEnvelope(data)
			if err != nil {
				logger.Warnf("Failed to unmarshal envelope: %v", err)
				continue
			}
			if common.HeaderType(channelHeader.Type) == common.HeaderType_CONFIG {
				// We can ignore config transactions as we only count data transactions.
				continue
			}
			res.Metrics.OnReceiveTransaction(channelHeader.TxId, protoblocktx.Status(statusCodes[i]))
		}
	}
}

// recapStatusCodes recaps of the status codes of a block.
func recapStatusCodes(statusCodes []byte) string {
	codes := make(map[byte]uint64)
	for _, code := range statusCodes {
		codes[code]++
	}
	items := make([]string, 0, len(codes))
	for code, count := range codes {
		items = append(
			items,
			fmt.Sprintf("%s x %d", protoblocktx.Status(code).String(), count),
		)
	}
	return strings.Join(items, ", ")
}
