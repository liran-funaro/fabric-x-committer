package verifierserver

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/pkg/errors"
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
	verifier      *sync.Map
	metrics       *metrics.Metrics
}

const (
	retValid = protoblocktx.Status_COMMITTED
	retError = -1
)

var (
	logger = logging.New("verifier")

	// ErrNoVerifier is returned when no verifier is set for a namespace.
	ErrNoVerifier = errors.New("no verifier set")
)

// New instantiate a new VerifierServer.
func New(parallelExecutionConfig *parallelexecutor.Config, m *metrics.Metrics) *VerifierServer {
	s := &VerifierServer{verifier: &sync.Map{}, metrics: m}

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
	for _, pd := range policies.Policies {
		nsID, key, err := policy.ParsePolicyItem(pd)
		if err != nil {
			return nil, err
		}
		verifier, err := signature.NewNsVerifier(key.GetScheme(), key.GetPublicKey())
		if err != nil {
			return nil, err
		}
		s.verifier.Store(nsID, verifier)
		logger.Infof("New verification key for namespace %d", nsID)
	}
	return &sigverification.Empty{}, nil
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
	status, err := s.verify(request.Tx)
	if err != nil {
		return nil, err
	}
	return &sigverification.Response{
		TxId:     request.Tx.Id,
		BlockNum: request.BlockNum,
		TxNum:    request.TxNum,
		Status:   status,
	}, nil
}

func (s *VerifierServer) verify(tx *protoblocktx.Tx) (protoblocktx.Status, error) {
	if status := verifyTxForm(tx); status != retValid {
		return status, nil
	}

	for nsIndex, ns := range tx.Namespaces {
		verifier, err := s.getVerifier(ns.NsId)
		if errors.Is(err, ErrNoVerifier) {
			return protoblocktx.Status_ABORTED_SIGNATURE_INVALID, nil
		} else if err != nil {
			// Internal error.
			return retError, err
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
		if err = verifier.VerifyNs(tx, nsIndex); err != nil {
			logger.Debugf("Invalid signature found: %v", tx.GetId())
			return protoblocktx.Status_ABORTED_SIGNATURE_INVALID, nil //nolint:nilerr
		}
	}

	return retValid, nil
}

func (s *VerifierServer) getVerifier(nsID string) (signature.NsVerifier, error) {
	v, ok := s.verifier.Load(nsID)
	if !ok {
		return nil, ErrNoVerifier
	}
	verifier, ok := v.(signature.NsVerifier)
	if !ok {
		return nil, errors.Errorf("verifier does not cast to signature.NsVerifier for namespace %s", nsID)
	}
	return verifier, nil
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
		ns, _, err := policy.ParsePolicyItem(pd)
		if err != nil {
			if errors.Is(err, types.ErrInvalidNamespaceID) {
				return protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID
			}
			return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
		}
		// The meta-namespace is updated only via blind-write.
		if ns == types.MetaNamespaceID {
			return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
		}
		if _, ok := nsUpdate[ns]; ok {
			return protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID
		}
		nsUpdate[ns] = nil
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
