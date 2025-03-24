package sigverification

import (
	"context"

	"github.com/cockroachdb/errors"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/grpcerror"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
)

// VerifierServer implements sigverification.VerifierServer.
type VerifierServer struct {
	protosigverifierservice.UnimplementedVerifierServer
	config  *Config
	metrics *metrics
}

const retValid = protoblocktx.Status_COMMITTED

var (
	logger = logging.New("verifier")

	// ErrUpdatePolicies is returned when UpdatePolicies fails to parse a given policy.
	ErrUpdatePolicies = errors.New("failed to update policies")
)

// New instantiate a new VerifierServer.
func New(config *Config) *VerifierServer {
	m := newMonitoring()
	s := &VerifierServer{
		config:  config,
		metrics: m,
	}
	return s
}

// Run the verifier background service.
func (s *VerifierServer) Run(ctx context.Context) error {
	_ = s.metrics.Provider.StartPrometheusServer(ctx, s.config.Monitoring.Server)
	// We don't return error here to avoid stopping the service due to monitoring error.
	// But we use the errgroup to ensure the method returns only when the server exits.
	return nil
}

// WaitForReady wait for service to be ready to be exposed as gRPC service.
// If the context ended before the service is ready, returns false.
func (*VerifierServer) WaitForReady(context.Context) bool {
	return true
}

// StartStream starts a verification stream.
func (s *VerifierServer) StartStream(stream protosigverifierservice.Verifier_StartStreamServer) error {
	logger.Infof("Starting new stream.")
	defer logger.Debug("Interrupted stream.")
	s.metrics.ActiveStreams.Inc()
	defer s.metrics.ActiveStreams.Dec()

	// We create a new executor for each stream to avoid answering to the wrong stream.
	executor := newParallelExecutor(&s.config.ParallelExecutor)
	g, gCtx := errgroup.WithContext(stream.Context())
	g.Go(func() error {
		return s.handleInputs(gCtx, stream, executor)
	})
	g.Go(func() error {
		return s.handleOutputs(gCtx, stream, executor)
	})
	g.Go(func() error {
		executor.handleCutoff(gCtx)
		return gCtx.Err()
	})
	for range executor.config.Parallelism {
		g.Go(func() error {
			executor.handleChannelInput(gCtx)
			return gCtx.Err()
		})
	}
	return grpcerror.WrapInternalError(g.Wait())
}

func (s *VerifierServer) handleInputs(
	ctx context.Context,
	stream protosigverifierservice.Verifier_StartStreamServer,
	executor *parallelExecutor,
) error {
	// ctx should be a child of stream.Context() so it will end with it.
	input := channel.NewWriter(ctx, executor.inputCh)
	for {
		batch, rpcErr := stream.Recv()
		if rpcErr != nil {
			return errors.Wrap(rpcErr, "stream ended")
		}
		logger.Debugf("Received input from client with %v requests", len(batch.Requests))
		err := executor.verifier.updatePolicies(batch.Update)
		if err != nil {
			return errors.Join(ErrUpdatePolicies, err)
		}
		s.metrics.VerifierServerInTxs.Add(len(batch.Requests))
		promutil.AddToGauge(s.metrics.ActiveRequests, len(batch.Requests))
		for _, r := range batch.Requests {
			if ok := input.Write(r); !ok {
				return errors.Wrap(stream.Context().Err(), "context ended")
			}
		}
	}
}

func (s *VerifierServer) handleOutputs(
	ctx context.Context,
	stream protosigverifierservice.Verifier_StartStreamServer,
	executor *parallelExecutor,
) error {
	// ctx should be a child of stream.Context() so it will end with it.
	output := channel.NewReader(ctx, executor.outputCh)
	for {
		outputs, ok := output.Read()
		if !ok {
			return errors.Wrap(stream.Context().Err(), "context ended")
		}
		s.metrics.VerifierServerOutTxs.Add(len(outputs))
		promutil.AddToGauge(s.metrics.ActiveRequests, -len(outputs))
		logger.Debugf("Received output: %v", output)
		rpcErr := stream.Send(&protosigverifierservice.ResponseBatch{Responses: outputs})
		if rpcErr != nil {
			return errors.Wrap(rpcErr, "stream ended")
		}
		logger.Debugf("Forwarded output to client.")
	}
}
