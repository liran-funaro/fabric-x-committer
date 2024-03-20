package verifierserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"time"

	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/streamhandler"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/zap/zapcore"
)

var logger = logging.New("verifierserver")

type verifierServer struct {
	sigverification.UnimplementedVerifierServer
	streamHandler *streamhandler.StreamHandler
	verifier      map[types.NamespaceID]*nsVerifierWithVersion
	metrics       *metrics.Metrics
}

type nsVerifierWithVersion struct {
	verifier signature.NsVerifier
	version  []byte
}

func New(parallelExecutionConfig *parallelexecutor.Config, m *metrics.Metrics) *verifierServer {
	s := &verifierServer{verifier: make(map[types.NamespaceID]*nsVerifierWithVersion), metrics: m}

	executor := parallelexecutor.New(s.verifyRequest, parallelExecutionConfig, m)
	s.streamHandler = streamhandler.New(
		func(batch *sigverification.RequestBatch) {
			if s.metrics.Enabled {
				m.VerifierServerInTxs.Add(len(batch.Requests))
				for _, request := range batch.Requests {
					if s.metrics.Enabled {
						s.metrics.RequestTracer.Start(request.GetTx().GetId())
					}
				}
			}
			executor.Submit(batch.Requests)
		},
		func() streamhandler.Output {
			outputs := <-executor.Outputs()
			if s.metrics.Enabled {
				m.VerifierServerOutTxs.Add(len(outputs))
				for _, output := range outputs {
					if s.metrics.Enabled {
						s.metrics.RequestTracer.End(output.GetTxId(), attribute.String(metrics.ValidLabel, metrics.ValidStatusMap[output.IsValid]))
					}
				}
			}
			return &sigverification.ResponseBatch{Responses: outputs}
		})
	return s
}

func (s *verifierServer) SetVerificationKey(context context.Context, verificationKey *sigverification.Key) (*sigverification.Empty, error) {
	if verificationKey == nil {
		logger.Info("Attempted to set an empty verification key.")
		return nil, errors.New("invalid public key")
	}
	verifier, err := signature.NewNsVerifier(verificationKey.GetScheme(), verificationKey.GetSerializedBytes())
	if err != nil {
		return nil, err
	}
	s.verifier[types.NamespaceID(verificationKey.NsId)] = &nsVerifierWithVersion{
		verifier: verifier,
		version:  verificationKey.NsVersion,
	}

	logger.Info("Set a new verification key.")
	return &sigverification.Empty{}, nil
}

func (s *verifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	logger.Infof("Starting new stream.")
	if s.metrics.Enabled {
		s.metrics.ActiveStreams.Inc()
		defer s.metrics.ActiveStreams.Dec()
	}
	s.streamHandler.HandleStream(stream)
	logger.Debug("Interrupted stream.")
	return nil
}

func (s *verifierServer) verifyRequest(request *sigverification.Request) (*sigverification.Response, error) {
	response := &sigverification.Response{
		TxId:     request.Tx.GetId(),
		BlockNum: request.GetBlockNum(),
		TxNum:    request.GetTxNum(),
		IsValid:  false,
	}

	start := time.Now()

	if len(request.Tx.Signatures) < len(request.Tx.Namespaces) {
		response.ErrorMessage = "not enough signatures"
		return response, nil
	}

	for nsIndex, ns := range request.Tx.Namespaces {
		v, ok := s.verifier[types.NamespaceID(ns.NsId)]
		if !ok {
			logger.Warnf("No verifier set! Returning invalid status.")
			response.ErrorMessage = "no verifier set"
			return response, nil
		}

		if !bytes.Equal(v.version, ns.NsVersion) {
			// TODO: Make the coordinator aware of the version mismatch
			//       and reject the transaction early.
			logger.Debugf(
				"Version of verifier [%d] mismatches with the namespace's version [%d]",
				v.version,
				ns.NsVersion,
			)
			return response, nil
		}

		if logger.Level() <= zapcore.DebugLevel {
			if data, err := json.Marshal(request.Tx); err != nil {
				logger.Debugf("Failed to marshal TX [%d:%d]", request.BlockNum, request.TxNum)
			} else {
				logger.Debugf("Requesting signature on TX [%d:%d]:\n%s", request.BlockNum, request.TxNum, string(data))
			}
		}

		if err := v.verifier.VerifyNs(request.Tx, nsIndex); err != nil {
			logger.Debugf("Invalid signature found: %v", request.GetTx().GetId())
			response.ErrorMessage = err.Error()
			return response, nil
		}
	}

	response.IsValid = true

	if s.metrics.Enabled {
		s.metrics.RequestTracer.AddEventAt(request.GetTx().GetId(), "Start verification", start)
		s.metrics.RequestTracer.AddEvent(request.GetTx().GetId(), "End verification")
	}
	return response, nil
}
