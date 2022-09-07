package verifierserver_test

import (
	"context"
	"log"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"google.golang.org/grpc"
)

const testTimeout = 3 * time.Second

type testState struct {
	parallelExecutionConfig *parallelexecutor.Config

	client     sigverification.VerifierClient
	stopClient func() error
	stopServer func()
}

var clientConnectionConfig = utils.NewDialConfig(utils.Endpoint{Host: "localhost", Port: config.GRPC_PORT})
var serverConnectionConfig = utils.ServerConfig{Endpoint: utils.Endpoint{Host: "localhost", Port: config.GRPC_PORT}}

func (s *testState) setUp(verificationScheme signature.Scheme) {
	clientConnection, _ := utils.Connect(clientConnectionConfig)
	s.client = sigverification.NewVerifierClient(clientConnection)
	s.stopClient = clientConnection.Close

	server := verifierserver.New(s.parallelExecutionConfig, verificationScheme)
	go func() {
		utils.RunServerMain(&serverConnectionConfig, func(grpcServer *grpc.Server) {
			s.stopServer = grpcServer.GracefulStop
			sigverification.RegisterVerifierServer(grpcServer, server)
		})
	}()
}

func (s *testState) tearDown() {
	err := s.stopClient()
	if err != nil {
		log.Fatalf("failed to close connection: %v", err)
	}
	s.stopServer()
}

var parallelExecutionConfig = &parallelexecutor.Config{
	BatchSizeCutoff:   3,
	BatchTimeCutoff:   1 * time.Hour,
	Parallelism:       3,
	ChannelBufferSize: 1,
}

func TestNoVerificationKeySet(t *testing.T) {
	registerFailHandler(t)
	c := &testState{parallelExecutionConfig: parallelExecutionConfig}
	c.setUp(signature.Ecdsa)

	stream, err := c.client.StartStream(context.Background())
	Expect(err).To(BeNil())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	_, err = stream.Recv()
	Expect(err).NotTo(BeNil())

	c.tearDown()
}

func TestNoInput(t *testing.T) {
	registerFailHandler(t)
	c := &testState{parallelExecutionConfig: parallelExecutionConfig}
	c.setUp(signature.Ecdsa)

	_, verificationKey := signature.NewSignerVerifier(signature.Ecdsa)

	_, err := c.client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	output := outputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(testTimeout).ShouldNot(Receive())

	c.tearDown()
}

func TestMinimalInput(t *testing.T) {
	registerFailHandler(t)
	c := &testState{parallelExecutionConfig: parallelExecutionConfig}
	c.setUp(signature.Ecdsa)

	sign, verificationKey := signature.NewSignerVerifier(signature.Ecdsa)

	_, err := c.client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: sign([][]byte{})},
		{BlockNum: 1, TxNum: 2, Tx: sign([][]byte{})},
		{BlockNum: 1, TxNum: 3, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}}},
	}})
	Expect(err).To(BeNil())

	output := outputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(1 * time.Second).Should(Receive(HaveLen(3)))

	c.tearDown()
}

func registerFailHandler(t *testing.T) {
	RegisterFailHandler(func(message string, callerSkip ...int) {
		t.Fatalf(message)
	})
}

func inputChannel(stream sigverification.Verifier_StartStreamClient) chan<- *sigverification.RequestBatch {
	channel := make(chan *sigverification.RequestBatch)
	go func() {
		for {
			batch := <-channel
			stream.Send(batch)
		}
	}()
	return channel
}

func outputChannel(stream sigverification.Verifier_StartStreamClient) <-chan []*sigverification.Response {
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
