package sigverification

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/cockroachdb/errors"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/channel"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/grpcerror"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/signature"
)

// VerifierServer implements sigverification.VerifierServer.
type VerifierServer struct {
	protosigverifierservice.UnimplementedVerifierServer
	verifiers atomic.Pointer[map[string]*signature.NsVerifier]
	config    *Config

	// Ensures a sequential updates.
	updateLock sync.Mutex

	metrics *metrics
}

const (
	retValid  = protoblocktx.Status_COMMITTED
	retConfig = protoblocktx.Status(-128)
)

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
	verifiers := make(map[string]*signature.NsVerifier)
	s.verifiers.Store(&verifiers)
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
	channelCapacity := s.config.ParallelExecutor.ChannelBufferSize * s.config.ParallelExecutor.Parallelism
	executor := &parallelExecutor{
		inputCh:        make(chan *protosigverifierservice.Request, channelCapacity),
		outputCh:       make(chan []*protosigverifierservice.Response),
		outputSingleCh: make(chan *protosigverifierservice.Response, channelCapacity),
		config:         &s.config.ParallelExecutor,
		executor:       s.verifyRequest,
	}
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
	return errors.Wrap(g.Wait(), "stream ended")
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

// UpdatePolicies updates the verifier's policies.
// If an error is returned, it will always be codes.InvalidArgument,
// indicating that the provided policies argument is invalid.
func (s *VerifierServer) UpdatePolicies(
	_ context.Context, policies *protoblocktx.Policies,
) (*protosigverifierservice.Empty, error) {
	// We parse the policy during validation and mark transactions as invalid if parsing fails.
	// While it might seem unlikely that policy parsing would fail at this stage, it could happen
	// if the stored policy in the database is corrupted or maliciously altered, or if there's a
	// bug in the committer that modifies the policy bytes.
	newVerifiers := make(map[string]*signature.NsVerifier)
	updatedNamespaces := make([]string, len(policies.Policies))
	for i, pd := range policies.Policies {
		key, err := policy.ParsePolicyItem(pd)
		if err != nil {
			return nil, grpcerror.WrapInvalidArgument(errors.Join(ErrUpdatePolicies, err))
		}
		nsVerifier, err := signature.NewNsVerifier(key.GetScheme(), key.GetPublicKey())
		if err != nil {
			return nil, grpcerror.WrapInvalidArgument(errors.Join(ErrUpdatePolicies, err))
		}
		newVerifiers[pd.Namespace] = nsVerifier
		updatedNamespaces[i] = pd.Namespace
	}

	defer logger.Infof("New verification policies for namespaces %v", updatedNamespaces)

	s.updateLock.Lock()
	defer s.updateLock.Unlock()
	for k, v := range *s.verifiers.Load() {
		if _, ok := newVerifiers[k]; !ok {
			newVerifiers[k] = v
		}
	}
	s.verifiers.Store(&newVerifiers)
	return nil, nil
}

func (s *VerifierServer) verifyRequest(request *protosigverifierservice.Request) *protosigverifierservice.Response {
	debug(request)
	return &protosigverifierservice.Response{
		TxId:     request.Tx.Id,
		BlockNum: request.BlockNum,
		TxNum:    request.TxNum,
		Status:   s.verify(request.Tx),
	}
}

func (s *VerifierServer) verify(tx *protoblocktx.Tx) protoblocktx.Status {
	status := verifyTxForm(tx)
	switch status {
	case retValid:
		// continue.
	case retConfig:
		// If we receive a valid config TX, we require a single empty signature.
		if len(tx.Signatures) != 1 || len(tx.Signatures[0]) != 0 {
			return protoblocktx.Status_ABORTED_SIGNATURE_INVALID
		}
		return retValid
	default:
		return status
	}

	// The verifiers might temporarily retain the old map while UpdatePolicies has already set a new one.
	// This is acceptable, provided the coordinator sends the validation status to the dependency graph
	// after updating the policies in the SigVerifier.
	// This ensures that dependent data transactions on these updated namespaces always use the map
	// containing the latest policy.
	verifiers := *s.verifiers.Load()
	for nsIndex, ns := range tx.Namespaces {
		nsVerifier, ok := verifiers[ns.NsId]
		if !ok {
			return protoblocktx.Status_ABORTED_SIGNATURE_INVALID
		}

		// NOTE: We do not compare the namespace version in the transaction
		//       against the namespace version in the verifier. This is because if
		//       the versions mismatch, and we reject the transaction, the coordinator
		//       would mark the transaction as invalid due to a bad signature. However,
		//       this may not be true if the policy was not actually updated with the
		//       new version. Hence, we should proceed to validate the signatures. If
		//       the signatures are valid, the validator-committer service would
		//       still mark the transaction as invalid due to an MVCC conflict on the
		//       namespace version, which would reflect the correct validation status.
		if err := nsVerifier.VerifyNs(tx, nsIndex); err != nil {
			logger.Debugf("Invalid signature found: %v", tx.GetId())
			return protoblocktx.Status_ABORTED_SIGNATURE_INVALID
		}
	}

	return retValid
}

func verifyTxForm(tx *protoblocktx.Tx) protoblocktx.Status {
	if tx.Id == "" {
		return protoblocktx.Status_ABORTED_MISSING_TXID
	}

	if len(tx.Namespaces) != len(tx.Signatures) {
		return protoblocktx.Status_ABORTED_SIGNATURE_INVALID
	}

	nsIDs := make(map[string]*protoblocktx.TxNamespace)
	for _, ns := range tx.Namespaces {
		if _, ok := nsIDs[ns.NsId]; ok {
			return protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE
		}
		if status := checkNamespaceFormation(ns); status != retValid {
			return status
		}
		nsIDs[ns.NsId] = ns
	}

	metaNs, ok := nsIDs[types.MetaNamespaceID]
	if ok {
		// TODO: need to decide whether we can allow other namespaces
		//       in the transaction when the MetaNamespaceID is present.
		// return len(nsIDs) == 1 && checkMetaNamespace(metaNs)
		return checkMetaNamespace(metaNs)
	}
	return retValid
}

func checkNamespaceFormation(ns *protoblocktx.TxNamespace) protoblocktx.Status {
	if ns.NsVersion == nil {
		return protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION
	}
	if len(ns.ReadWrites) == 0 && len(ns.BlindWrites) == 0 {
		return protoblocktx.Status_ABORTED_NO_WRITES
	}
	return retValid
}

func checkMetaNamespace(txNs *protoblocktx.TxNamespace) protoblocktx.Status {
	if len(txNs.ReadWrites) > 0 {
		// If we have read-writes, we classify this as namespace policy update.
		// Thus, blind-writes are not allowed.
		if len(txNs.BlindWrites) > 0 {
			return protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED
		}

		nsUpdate := make(map[string]any)
		for _, pd := range policy.ListPolicyItems(txNs.ReadWrites) {
			_, err := policy.ParsePolicyItem(pd)
			if err != nil {
				if errors.Is(err, policy.ErrInvalidNamespaceID) {
					return protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID
				}
				return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
			}
			// The meta-namespace is updated only via blind-write.
			if pd.Namespace == types.MetaNamespaceID {
				return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
			}
			if _, ok := nsUpdate[pd.Namespace]; ok {
				return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
			}
			nsUpdate[pd.Namespace] = nil
		}

		return retValid
	}

	// If we have blind writes, we classify it as meta-namespace update.
	// Thus, we allow only a single blind-write, and no read-writes.
	if len(txNs.BlindWrites) > 1 {
		return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
	}

	for _, pd := range policy.ListPolicyItems(txNs.BlindWrites) {
		_, err := policy.ParsePolicyItem(pd)
		if err != nil {
			return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
		}
		// A non meta-namespace is updated only via read-write.
		if pd.Namespace != types.MetaNamespaceID {
			return protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED
		}
	}

	return retConfig
}

func debug(request *protosigverifierservice.Request) {
	if logger.Level() > zapcore.DebugLevel {
		return
	}
	data, err := json.Marshal(request.Tx)
	if err != nil {
		logger.Debugf("Failed to marshal TX [%d:%d]", request.BlockNum, request.TxNum)
		return
	}
	logger.Debugf("Validating TX [%d:%d]:\n%s", request.BlockNum, request.TxNum, string(data))
}
