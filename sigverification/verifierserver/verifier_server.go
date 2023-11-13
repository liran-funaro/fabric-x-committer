package verifierserver

import (
	"context"
	"errors"
	"time"

	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/streamhandler"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"go.opentelemetry.io/otel/attribute"
)

var logger = logging.New("verifierserver")

type verifierServer struct {
	sigverification.UnimplementedVerifierServer
	verificationScheme signature.Scheme
	streamHandler      *streamhandler.StreamHandler
	verifier           signature.TxVerifier
	metrics            *metrics.Metrics
}

func New(parallelExecutionConfig *parallelexecutor.Config, verificationScheme signature.Scheme, m *metrics.Metrics) *verifierServer {
	s := &verifierServer{verificationScheme: verificationScheme, metrics: m}

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
	logger.Infof("Verifier server created with scheme: %s", verificationScheme)
	return s
}

func (s *verifierServer) SetVerificationKey(context context.Context, verificationKey *sigverification.Key) (*sigverification.Empty, error) {
	if verificationKey == nil {
		logger.Info("Attempted to set an empty verification key.")
		return nil, errors.New("invalid public key")
	}
	verifier, err := signature.NewTxVerifier(s.verificationScheme, verificationKey.GetSerializedBytes())
	if err != nil {
		return nil, err
	}
	s.verifier = verifier

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

	if s.verifier == nil {
		logger.Warnf("No verifier set! Returning invalid status.")
		response.ErrorMessage = "no verifier set"
		return response, nil
	}

	if err := s.verifier.VerifyTx(request.Tx); err != nil {
		logger.Debugf("Invalid signature found: %v", request.GetTx().GetId())
		response.ErrorMessage = err.Error()
	} else {
		response.IsValid = true
	}

	if s.metrics.Enabled {
		s.metrics.RequestTracer.AddEventAt(request.GetTx().GetId(), "Start verification", start)
		s.metrics.RequestTracer.AddEvent(request.GetTx().GetId(), "End verification")
	}
	return response, nil
}
