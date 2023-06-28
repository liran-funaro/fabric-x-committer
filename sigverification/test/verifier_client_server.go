package sigverification_test

import (
	"log"
	"sync"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

// Constants

// De facto constants

var SerialNumberCountDistribution = test.Constant(int64(test.TxSize))
var SerialNumberSize = test.Constant(32)
var OutputCountDistribution = test.Constant(int64(test.TxSize))
var OutputSize = test.Constant(32)
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

func NewTestState(server sigverification.VerifierServer) *State {
	serverConnectionConfig := connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost", Port: 0}}
	s := &State{}

	var port int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		connection.RunServerMain(&serverConnectionConfig, func(grpcServer *grpc.Server, actualListeningPort int) {
			s.StopServer = grpcServer.GracefulStop
			port = actualListeningPort
			sigverification.RegisterVerifierServer(grpcServer, server)
			wg.Done()
		})
	}()
	wg.Wait()

	clientConnectionConfig := connection.NewDialConfig(connection.Endpoint{Host: "localhost", Port: port})
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
