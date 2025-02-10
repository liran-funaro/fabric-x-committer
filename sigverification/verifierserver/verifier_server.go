package verifierserver

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/streamhandler"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap/zapcore"
)

// VerifierServer implements sigverification.VerifierServer.
type VerifierServer struct {
	sigverification.UnimplementedVerifierServer
	streamHandler *streamhandler.StreamHandler
	verifiers     atomic.Pointer[map[string]signature.NsVerifier]

	// Ensures a sequential updates.
	updateLock sync.Mutex

	metrics *metrics.Metrics
}

const (
	retValid = protoblocktx.Status_COMMITTED
)

var (
	logger = logging.New("verifier")

	// ErrUpdatePolicies is returned when UpdatePolicies fails to parse a given policy.
	ErrUpdatePolicies = errors.New("failed to update policies")
)

// New instantiate a new VerifierServer.
func New(parallelExecutionConfig *parallelexecutor.Config, m *metrics.Metrics) *VerifierServer {
	s := &VerifierServer{metrics: m}
	verifiers := make(map[string]signature.NsVerifier)
	s.verifiers.Store(&verifiers)

	executor := parallelexecutor.New(s.verifyRequest, parallelExecutionConfig, m)
	s.streamHandler = streamhandler.New(
		func(batch *sigverification.RequestBatch) {
			if s.metrics.Enabled {
				m.VerifierServerInTxs.Add(len(batch.Requests))
				for _, request := range batch.Requests {
					s.metrics.RequestTracer.Start(request.Tx.Id)
				}
			}
			executor.Submit(batch.Requests)
		},
		func() streamhandler.Output {
			outputs := <-executor.Outputs()
			if s.metrics.Enabled {
				m.VerifierServerOutTxs.Add(len(outputs))
				for _, o := range outputs {
					s.metrics.RequestTracer.End(o.TxId, attribute.String(metrics.ValidLabel, o.Status.String()))
				}
			}
			return &sigverification.ResponseBatch{Responses: outputs}
		})
	return s
}

// UpdatePolicies updates the policies of the verifier.
func (s *VerifierServer) UpdatePolicies(
	_ context.Context, policies *sigverification.Policies,
) (*sigverification.Empty, error) {
	// We parse the policy during validation and mark transactions as invalid if parsing fails.
	// While it might seem unlikely that policy parsing would fail at this stage, it could happen
	// if the stored policy in the database is corrupted or maliciously altered, or if there's a
	// bug in the committer that modifies the policy bytes.
	newVerifiers := make(map[string]signature.NsVerifier)
	updatedNamespaces := make([]string, len(policies.Policies))
	for i, pd := range policies.Policies {
		key, err := policy.ParsePolicyItem(pd)
		if err != nil {
			return nil, errors.Join(ErrUpdatePolicies, err)
		}
		verifier, err := signature.NewNsVerifier(key.GetScheme(), key.GetPublicKey())
		if err != nil {
			return nil, errors.Join(ErrUpdatePolicies, err)
		}
		newVerifiers[pd.Namespace] = verifier
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

// StartStream starts a verification stream.
func (s *VerifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	logger.Infof("Starting new stream.")
	if s.metrics.Enabled {
		s.metrics.ActiveStreams.Inc()
		defer s.metrics.ActiveStreams.Dec()
	}
	s.streamHandler.HandleStream(stream)
	logger.Debug("Interrupted stream.")
	return nil
}

func (s *VerifierServer) verifyRequest(request *sigverification.Request) (*sigverification.Response, error) {
	if s.metrics.Enabled {
		txID := request.Tx.Id
		s.metrics.RequestTracer.AddEvent(txID, "Start verification")
		defer s.metrics.RequestTracer.AddEvent(txID, "End verification")
	}
	debug(request)
	return &sigverification.Response{
		TxId:     request.Tx.Id,
		BlockNum: request.BlockNum,
		TxNum:    request.TxNum,
		Status:   s.verify(request.Tx),
	}, nil
}

func (s *VerifierServer) verify(tx *protoblocktx.Tx) protoblocktx.Status {
	if status := verifyTxForm(tx); status != retValid {
		return status
	}

	// The verifiers might temporarily retain the old map while UpdatePolicies has already set a new one.
	// This is acceptable, provided the coordinator sends the validation status to the dependency graph
	// after updating the policies in the SigVerifier.
	// This ensures that dependent data transactions on these updated namespaces always use the map
	// containing the latest policy.
	verifiers := *s.verifiers.Load()
	for nsIndex, ns := range tx.Namespaces {
		verifier, ok := verifiers[ns.NsId]
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
		if err := verifier.VerifyNs(tx, nsIndex); err != nil {
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
	// Blind-writes are not allowed.
	if len(txNs.BlindWrites) > 0 {
		return protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED
	}

	nsUpdate := make(map[string]any)
	for _, pd := range policy.ListPolicyItems(txNs.ReadWrites) {
		_, err := policy.ParsePolicyItem(pd)
		if err != nil {
			if errors.Is(err, types.ErrInvalidNamespaceID) {
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

func debug(request *sigverification.Request) {
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
