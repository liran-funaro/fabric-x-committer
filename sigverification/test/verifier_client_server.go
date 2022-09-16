package sigverification_test

import (
	"log"

	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

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

func OutputChannel(stream sigverification.Verifier_StartStreamClient) <-chan []*sigverification.Response {
	output := make(chan []*sigverification.Response)
	go func() {
		for {
			response, _ := stream.Recv()
			if response == nil || response.Responses == nil {
				return
			}
			if len(response.Responses) > 0 {
				output <- response.Responses
			}
		}
	}()
	return output
}
