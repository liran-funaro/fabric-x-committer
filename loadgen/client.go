package loadgen

import (
	"context"
	"time"

	"github.com/pkg/errors"
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
		namespaceGen   *workload.NamespaceGenerator
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
		namespaceChan channel.Reader[*protoblocktx.Tx]
		txStream      *workload.TxStream
		blockSize     uint64
	}

	// clientBlockGenerator is a block generator that first submit a block with the namespace TX,
	// and blocks until indicated that it was committed.
	// Then, it submits blocks from the tx stream.
	clientBlockGenerator struct {
		namespaceChan channel.Reader[*protoblocktx.Tx]
		blockGen      workload.Generator[*protoblocktx.Block]
	}

	// clientTxGenerator is a TX generator that first submit the namespace TX,
	// and blocks until indicated that it was committed.
	// Then, it submits transactions from the tx stream.
	clientTxGenerator struct {
		namespaceChan channel.Reader[*protoblocktx.Tx]
		txGen         workload.Generator[*protoblocktx.Tx]
	}
)

var logger = logging.New("load-gen-client")

// NewLoadGenClient creates a new client instance.
func NewLoadGenClient(conf *ClientConfig) (*Client, error) {
	logger.Infof("Config passed: %s", utils.LazyJson(conf))
	if conf.OnlyNamespaceGeneration && conf.OnlyLoadGeneration {
		return nil, errors.New("only one of OnlyNamespaceGeneration and OnlyLoadGeneration must be specified")
	}
	c := &Client{
		conf:           conf,
		txStream:       workload.NewTxStream(conf.LoadProfile, conf.Stream),
		namespaceGen:   workload.NewNamespaceGenerator(conf.LoadProfile.Transaction.Signature),
		metricProvider: metrics.CreateProvider(conf.Monitoring.Metrics),
		resources: adapters.ClientResources{
			Profile: conf.LoadProfile,
		},
	}
	c.resources.Metrics = metrics.NewLoadgenServiceMetrics(c.metricProvider)
	c.resources.PublicKey, c.resources.KeyScheme = c.txStream.Signer.HashSigner.GetVerificationKey()

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

	cs := &clientStream{
		blockSize: c.conf.LoadProfile.Block.Size,
	}
	var namespaceChan chan *protoblocktx.Tx
	if !c.conf.OnlyLoadGeneration {
		namespaceChan = make(chan *protoblocktx.Tx, 1)
		cs.namespaceChan = channel.NewReader(gCtx, namespaceChan)
	}
	if !c.conf.OnlyNamespaceGeneration {
		cs.txStream = c.txStream
	}
	g.Go(func() error {
		return c.adapter.RunWorkload(gCtx, cs)
	})

	if !c.conf.OnlyLoadGeneration {
		err := c.submitNamespaceTx(ctx, namespaceChan)
		if err != nil {
			cancel()
			if gErr := g.Wait(); gErr != nil {
				return errors.Wrapf(gErr, "failed to submit namespace TX")
			}
			return err
		}
	}

	if c.conf.OnlyNamespaceGeneration {
		cancel()
	}
	return g.Wait()
}

// submitNamespaceTx writes the namespace TX to the channel, and waits for it to be committed.
func (c *Client) submitNamespaceTx(ctx context.Context, namespaceChan chan *protoblocktx.Tx) error {
	namespaceTx, err := c.namespaceGen.CreateNamespaces()
	if err != nil {
		return errors.Wrap(err, "failed to create namespace tx")
	}
	logger.Infof("Submitting namespace TX")
	lastProgress := c.adapter.Progress()
	if !channel.NewWriter(ctx, namespaceChan).Write(namespaceTx) {
		return errors.New("context ended before submitting namespace TX")
	}

	for c.adapter.Progress() == lastProgress {
		select {
		case <-ctx.Done():
			return errors.New("context ended before acknowledging namespace TX")
		case <-time.Tick(time.Second):
		}
	}

	logger.Infof("Namespace TX submitted")
	close(namespaceChan)
	return nil
}

// MakeBlocksGenerator instantiate clientBlockGenerator.
func (c *clientStream) MakeBlocksGenerator() workload.Generator[*protoblocktx.Block] {
	cg := &clientBlockGenerator{
		namespaceChan: c.namespaceChan,
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
		namespaceChan: c.namespaceChan,
	}
	if c.txStream != nil {
		cg.txGen = c.txStream.MakeGenerator()
	}
	return cg
}

// Next generate the next block.
func (g *clientBlockGenerator) Next() *protoblocktx.Block {
	if g.namespaceChan != nil {
		if namespaceTx, ok := g.namespaceChan.Read(); ok {
			return &protoblocktx.Block{
				Txs:    []*protoblocktx.Tx{namespaceTx},
				TxsNum: []uint32{0},
			}
		}
		g.namespaceChan = nil
	}
	if g.blockGen != nil {
		return g.blockGen.Next()
	}
	return nil
}

// Next generate the next TX.
func (g *clientTxGenerator) Next() *protoblocktx.Tx {
	if g.namespaceChan != nil {
		if namespaceTx, ok := g.namespaceChan.Read(); ok {
			return namespaceTx
		}
		g.namespaceChan = nil
	}
	if g.txGen != nil {
		return g.txGen.Next()
	}
	return nil
}
