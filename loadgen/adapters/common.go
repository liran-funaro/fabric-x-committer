package adapters

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

type (
	// ClientResources holds client's pre-generated resources to be used by the adapters.
	ClientResources struct {
		Metrics *metrics.PerfMetrics
		Profile *workload.Profile
	}

	// TxStream makes generators such that all can be used in parallel.
	TxStream interface {
		MakeTxGenerator() workload.Generator[*protoblocktx.Tx]
		MakeBlocksGenerator() workload.Generator[*protoblocktx.Block]
	}

	// commonAdapter is used as a base class for the adapters.
	commonAdapter struct {
		res          *ClientResources
		nextBlockNum atomic.Uint64
	}
)

var (
	logger = logging.New("load-gen-adapters")

	// ErrInvalidAdapterConfig indicates that none of the adapter's config were given.
	ErrInvalidAdapterConfig = errors.New("invalid config passed")
)

// Progress a committed transaction indicate progress for most adapters.
func (c *commonAdapter) Progress() uint64 {
	committed, err := c.res.Metrics.GetCommitted()
	if err != nil {
		return 0
	}
	return committed
}

func (c *commonAdapter) sendBlocks(
	ctx context.Context,
	txStream TxStream,
	send func(*protoblocktx.Block) error,
) error {
	blockGen := txStream.MakeBlocksGenerator()
	for ctx.Err() == nil {
		block := blockGen.Next()
		if block == nil {
			// If the context ended, the generator returns nil.
			break
		}
		block.Number = c.nextBlockNum.Add(1) - 1
		logger.Debugf("Sending block %d with %d TXs", block.Number, len(block.Txs))
		if err := send(block); err != nil {
			return connection.FilterStreamRPCError(err)
		}
		c.res.Metrics.OnSendBlock(block)
	}
	return nil
}

// setupCoordinator this will be removed once the coordinator will process config TXs.
func (c *commonAdapter) setupCoordinator(ctx context.Context, client protocoordinatorservice.CoordinatorClient) error {
	txSigner := workload.NewTxSignerVerifier(c.res.Profile.Transaction.Policy)
	signer, ok := txSigner.HashSigners[types.MetaNamespaceID]
	if !ok {
		return errors.New("no meta namespace signer found")
	}
	_, err := client.UpdatePolicies(ctx, &protosigverifierservice.Policies{
		Policies: []*protosigverifierservice.PolicyItem{{
			Namespace: types.MetaNamespaceID.Bytes(),
			Policy:    protoutil.MarshalOrPanic(signer.GetVerificationPolicy()),
		}},
	})
	if err != nil {
		return err
	}
	return nil
}
