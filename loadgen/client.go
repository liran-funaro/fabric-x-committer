package loadgen

import (
	"context"
	"log"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

// ErrStoppedByUser is returned if the client terminated by request.
var ErrStoppedByUser = errors.New("stopped by the user")

type (
	// BlockGenClient is the interface for all supported clients.
	BlockGenClient interface {
		// Start workload and namespace-initialization generator clients in the background.
		Start(*BlockStream, *NamespaceGenerator) error
		// Stop the workload generator.
		Stop()
		// Context returns the used context.
		Context() context.Context

		startNamespaceGeneration(*NamespaceGenerator) error
		startWorkload(*BlockStream) error
	}

	loadGenClient struct {
		ctx    context.Context
		cancel context.CancelCauseFunc
	}

	// LoadBundle contains utilities for the loadgen client.
	LoadBundle struct {
		Metrics      *PerfMetrics
		BlkStream    *BlockStream
		NamespaceGen *NamespaceGenerator
		Client       BlockGenClient
	}
)

func newLoadGenClient() *loadGenClient {
	ctx, cancel := context.WithCancelCause(context.Background())
	return &loadGenClient{
		ctx:    ctx,
		cancel: cancel,
	}
}

func (c *loadGenClient) Stop() {
	c.cancel(ErrStoppedByUser)
}

func (c *loadGenClient) Context() context.Context {
	return c.ctx
}

func createClient(c *ClientConfig) (BlockGenClient, *PerfMetrics, error) { //nolint:ireturn
	logger.Infof("Config passed: %s", utils.LazyJson(c))
	m := newBlockgenServiceMetrics(createProvider(c.Monitoring.Metrics))

	if c.CoordinatorClient != nil {
		return newCoordinatorClient(c.CoordinatorClient, m), m, nil
	}
	if c.VCClient != nil {
		return newVCClient(c.VCClient, m), m, nil
	}
	if c.SidecarClient != nil {
		return newSidecarClient(c.SidecarClient, m), m, nil
	}
	if c.SigVerifierClient != nil {
		return newSVClient(c.SigVerifierClient, m), m, nil
	}
	return nil, nil, errors.New("invalid config passed")
}

func (c *loadGenClient) startSending(
	blockGen Generator[*protoblocktx.Block], stream grpc.ClientStream, send func(*protoblocktx.Block) error,
) {
	defer func() {
		_ = stream.CloseSend()
	}()
	for c.ctx.Err() == nil {
		block := blockGen.Next()
		if block == nil {
			// If the context ended, the block generator returns nil.
			logger.Infof("block generator terminated")
			break
		}
		if err := send(block); err != nil {
			c.cancel(connection.WrapStreamRpcError(err))
		}
	}
}

// Starter starts the load generator with the given configuration.
func Starter( //nolint:revive,ireturn
	c *ClientConfig,
) (LoadBundle, error) {
	client, perfMetrics, err := createClient(c)
	if err != nil {
		return LoadBundle{}, errors.Wrap(err, "failed creating client")
	}

	go func() {
		errProm := perfMetrics.provider.StartPrometheusServer(client.Context())
		if errProm != nil {
			log.Panic(err) // nolint: revive
		}
	}()

	return LoadBundle{
		Metrics:      perfMetrics,
		BlkStream:    StartBlockGenerator(client.Context(), c.LoadProfile, c.Stream),
		NamespaceGen: NewNamespaceGenerator(c.LoadProfile.Transaction.Signature),
		Client:       client,
	}, nil
}
