package adapters

import (
	"context"
	"errors"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
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

	// Phases specify the generation phases to enable.
	Phases struct {
		Config     bool `mapstructure:"config" yaml:"config"`
		Namespaces bool `mapstructure:"namespaces" yaml:"namespaces"`
		Load       bool `mapstructure:"load" yaml:"load"`
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

// PhasesIntersect returns the phases that are both in p1 and p2.
// If one of them is empty, it is assumed not to be specified.
func PhasesIntersect(p1, p2 Phases) Phases {
	if p1.Empty() {
		return p2
	}
	if p2.Empty() {
		return p1
	}
	return Phases{
		Config:     p1.Config && p2.Config,
		Namespaces: p1.Namespaces && p2.Namespaces,
		Load:       p1.Load && p2.Load,
	}
}

// Empty returns true if no phase was applied.
func (p *Phases) Empty() bool {
	return p == nil || (!p.Config && !p.Namespaces && !p.Load)
}

// Progress a committed transaction indicate progress for most adapters.
func (c *commonAdapter) Progress() uint64 {
	committed, err := c.res.Metrics.GetCommitted()
	if err != nil {
		return 0
	}
	return committed
}

// Supports specify which phases an adapter supports.
func (*commonAdapter) Supports() Phases {
	return Phases{
		Config:     true,
		Namespaces: true,
		Load:       true,
	}
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
