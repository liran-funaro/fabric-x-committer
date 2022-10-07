package sigverification_test

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"log"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

// Constants

// De facto constants

var TxSizeDistribution = test.Constant(int64(test.TxSize))
var SerialNumberSize = test.Constant(32)
var SignatureValidRatio = test.Always
var BatchSizeDistribution = test.Constant(int64(test.BatchSize))
var VerificationScheme = signature.Ecdsa

// Typical result values from experiments

var TypicalTxValidationDelay = int64(time.Second / 10_000)

// Optimal values resulting from benchmarking

var OptimalBatchTimeCutoff = 500 * time.Millisecond
var OptimalChannelBufferSize = 50

type State struct {
	Client     sigverification.VerifierClient
	StopClient func() error
	StopServer func()
}

var clientConnectionConfig = connection.NewDialConfig(connection.Endpoint{Host: "localhost", Port: 5001})
var serverConnectionConfig = connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost", Port: 5001}}

func NewTestState(server sigverification.VerifierServer) *State {
	s := &State{}
	clientConnection, _ := connection.Connect(clientConnectionConfig)
	s.Client = sigverification.NewVerifierClient(clientConnection)
	s.StopClient = clientConnection.Close

	go func() {
		connection.RunServerMain(&serverConnectionConfig, func(grpcServer *grpc.Server) {
			s.StopServer = grpcServer.GracefulStop
			sigverification.RegisterVerifierServer(grpcServer, server)
		})
	}()
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
