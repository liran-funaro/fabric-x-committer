package loadgen

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	monitoringmetrics "github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"golang.org/x/sync/errgroup"
)

type (
	// Client for applying load on the services.
	Client struct {
		conf           *ClientConfig
		txStream       *workload.TxStream
		metricProvider monitoringmetrics.Provider
		resources      adapters.ClientResources
		adapter        ServiceAdapter
	}

	// ServiceAdapter encapsulates the common interface for adapters.
	ServiceAdapter interface {
		// RunWorkload apply the generated workload.
		RunWorkload(ctx context.Context, txStream adapters.TxStream) error
		// Progress returns a value that indicates progress as the number grows.
		Progress() uint64
	}

	// clientStream implements the TxStream interface.
	clientStream struct {
		metaChan  channel.Reader[*protoblocktx.Tx]
		txStream  *workload.TxStream
		blockSize uint64
	}

	// clientBlockGenerator is a block generator that first submit blocks from the metaChan,
	// and blocks until indicated that it was committed.
	// Then, it submits blocks from the tx stream.
	clientBlockGenerator struct {
		metaChan channel.Reader[*protoblocktx.Tx]
		blockGen workload.Generator[*protoblocktx.Block]
	}

	// clientTxGenerator is a TX generator that first submit TXs from the metaChan,
	// and blocks until indicated that it was committed.
	// Then, it submits transactions from the tx stream.
	clientTxGenerator struct {
		metaChan channel.Reader[*protoblocktx.Tx]
		txGen    workload.Generator[*protoblocktx.Tx]
	}
)

var (
	logger          = logging.New("load-gen-client")
	defaultGenerate = Generate{
		Namespaces: true,
		Load:       true,
	}
)

// NewLoadGenClient creates a new client instance.
func NewLoadGenClient(conf *ClientConfig) (*Client, error) {
	logger.Infof("Config passed: %s", utils.LazyJson(conf))
	if conf.Generate == nil || !(conf.Generate.Load || conf.Generate.Namespaces) {
		conf.Generate = &defaultGenerate
	}

	c := &Client{
		conf:           conf,
		txStream:       workload.NewTxStream(conf.LoadProfile, conf.Stream),
		metricProvider: metrics.CreateProvider(conf.Monitoring.Metrics),
		resources: adapters.ClientResources{
			Profile: conf.LoadProfile,
		},
	}
	c.resources.Metrics = metrics.NewLoadgenServiceMetrics(c.metricProvider)

	adapter, err := getAdapter(&conf.Adapter, &c.resources)
	if err != nil {
		return nil, err
	}

	c.adapter = adapter
	return c, nil
}

func getAdapter(conf *adapters.AdapterConfig, res *adapters.ClientResources) (ServiceAdapter, error) {
	switch {
	case conf.CoordinatorClient != nil:
		return adapters.NewCoordinatorAdapter(conf.CoordinatorClient, res), nil
	case conf.VCClient != nil:
		return adapters.NewVCAdapter(conf.VCClient, res), nil
	case conf.SidecarClient != nil:
		return adapters.NewSidecarAdapter(conf.SidecarClient, res), nil
	case conf.SigVerifierClient != nil:
		return adapters.NewSVAdapter(conf.SigVerifierClient, res), nil
	default:
		return nil, adapters.ErrInvalidAdapterConfig
	}
}

// Run applies load on the service.
func (c *Client) Run(ctx context.Context) error {
	logger.Infof("Starting workload generation")
	defer logger.Infof("End workload generation")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return c.metricProvider.StartPrometheusServer(gCtx)
	})
	g.Go(func() error {
		return c.txStream.Run(gCtx)
	})
	c.txStream.WaitForReady(gCtx)

	metaChan := make(chan *protoblocktx.Tx, 1)
	cs := &clientStream{
		blockSize: c.conf.LoadProfile.Block.Size,
		metaChan:  channel.NewReader(gCtx, metaChan),
	}
	if c.conf.Generate.Load {
		cs.txStream = c.txStream
	}
	g.Go(func() error {
		return c.adapter.RunWorkload(gCtx, cs)
	})

	if err := c.submitMetaTXs(ctx, metaChan); err != nil {
		logger.Errorf("Failed to process meta tx: %v", err)
		cancel()
		if gErr := g.Wait(); gErr != nil {
			return fmt.Errorf("failed to process meta TX: %w", gErr)
		}
		return err
	}

	if !c.conf.Generate.Load {
		cancel()
	}
	return g.Wait()
}

// submitMetaTXs writes the meta TXs to the channel, and waits for them to be committed.
func (c *Client) submitMetaTXs(ctx context.Context, metaChanIn chan *protoblocktx.Tx) error {
	defer close(metaChanIn)
	if !c.conf.Generate.Namespaces {
		return nil
	}

	metaChan := channel.NewWriter(ctx, metaChanIn)
	curProgress := c.adapter.Progress()

	meta, err := workload.CreateNamespaces(c.conf.LoadProfile.Transaction.Policy)
	if err != nil {
		return err
	}

	logger.Infof("Submitting meta TX. Progress: %d", curProgress)
	if !metaChan.Write(meta) {
		return errors.New("context ended before submitting meta TX")
	}

	for lastProgress := curProgress; curProgress == lastProgress; curProgress = c.adapter.Progress() {
		select {
		case <-ctx.Done():
			return errors.New("context ended before acknowledging meta TX")
		case <-time.Tick(time.Second):
		}
	}
	logger.Infof("Meta TX submitted. Progress: %d", curProgress)
	return nil
}

// MakeBlocksGenerator instantiate clientBlockGenerator.
func (c *clientStream) MakeBlocksGenerator() workload.Generator[*protoblocktx.Block] {
	cg := &clientBlockGenerator{
		metaChan: c.metaChan,
	}
	if c.txStream != nil {
		cg.blockGen = &workload.BlockGenerator{
			TxGenerator: c.txStream.MakeGenerator(),
			BlockSize:   c.blockSize,
		}
	}
	return cg
}

// MakeTxGenerator instantiate clientTxGenerator.
func (c *clientStream) MakeTxGenerator() workload.Generator[*protoblocktx.Tx] {
	cg := &clientTxGenerator{
		metaChan: c.metaChan,
	}
	if c.txStream != nil {
		cg.txGen = c.txStream.MakeGenerator()
	}
	return cg
}

// Next generate the next block.
func (g *clientBlockGenerator) Next() *protoblocktx.Block {
	if g.metaChan != nil {
		if metaTX, ok := g.metaChan.Read(); ok {
			return &protoblocktx.Block{
				Txs:    []*protoblocktx.Tx{metaTX},
				TxsNum: []uint32{0},
			}
		}
		g.metaChan = nil
	}
	if g.blockGen != nil {
		return g.blockGen.Next()
	}
	return nil
}

// Next generate the next TX.
func (g *clientTxGenerator) Next() *protoblocktx.Tx {
	if g.metaChan != nil {
		if metaTx, ok := g.metaChan.Read(); ok {
			return metaTx
		}
		g.metaChan = nil
	}
	if g.txGen != nil {
		return g.txGen.Next()
	}
	return nil
}
