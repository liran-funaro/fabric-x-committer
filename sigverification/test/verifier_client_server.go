package sigverification_test

import (
	"log"
	"time"

	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

// Constants

// De facto constants

var (
	SerialNumberCountDistribution = test.Constant(int64(test.TxSize))
	SerialNumberSize              = test.Constant(32)
	OutputCountDistribution       = test.Constant(int64(test.TxSize))
	OutputSize                    = test.Constant(32)
	SignatureValidRatio           = test.Always
	BatchSizeDistribution         = test.Constant(int64(test.BatchSize))
	VerificationScheme            = signature.Ecdsa
)

// Typical result values from experiments

var TypicalTxValidationDelay = int64(time.Second / 10_000)

// Optimal values resulting from benchmarking

var (
	OptimalBatchTimeCutoff   = 500 * time.Millisecond
	OptimalChannelBufferSize = 50
)

type State struct {
	Client     sigverification.VerifierClient
	StopClient func() error
	StopServer func()
}

func NewTestState(t test.TestingT, server sigverification.VerifierServer) *State {
	serverConnectionConfig := connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost", Port: 0}}
	s := &State{}

	test.RunGrpcServerForTest(t, &serverConnectionConfig, func(grpcServer *grpc.Server) {
		s.StopServer = grpcServer.GracefulStop
		sigverification.RegisterVerifierServer(grpcServer, server)
	})

	clientConnectionConfig := connection.NewDialConfig(&serverConnectionConfig.Endpoint)
	clientConnection, _ := connection.Connect(clientConnectionConfig)
	s.Client = sigverification.NewVerifierClient(clientConnection)
	s.StopClient = clientConnection.Close
	return s
}

func (s *State) TearDown() {
	err := s.StopClient()
	if err != nil {
		log.Fatalf("failed to close connection: %v", err)
	}
	s.StopServer()
}

func InputChannel(stream sigverification.Verifier_StartStreamClient) chan<- *sigverification.RequestBatch {
	channel := make(chan *sigverification.RequestBatch)
	go func() {
		for {
			batch := <-channel
			stream.Send(batch)
		}
	}()
	return channel
}
