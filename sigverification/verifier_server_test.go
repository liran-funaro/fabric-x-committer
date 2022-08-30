package sigverification_test

import (
	"context"
	"log"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"google.golang.org/grpc"
)

const testTimeout = 3 * time.Second

type testState struct {
	testing                 *testing.T
	parallelExecutionConfig *sigverification.ParallelExecutionConfig

	client     sigverification.VerifierClient
	stopClient func() error
	stopServer func()

	signer sigverification.TxSigner
}

var clientConnectionConfig = utils.NewDialConfig(utils.Endpoint{Host: "localhost", Port: config.GRPC_PORT})
var serverConnectionConfig = utils.ServerConfig{Endpoint: utils.Endpoint{Host: "localhost", Port: config.GRPC_PORT}}
var verificationScheme = sigverification.Ecdsa

func (s *testState) setUp() {
	RegisterFailHandler(func(message string, callerSkip ...int) {
		s.testing.Fatalf(message)
	})

	clientConnection, _ := utils.Connect(clientConnectionConfig)
	s.client = sigverification.NewVerifierClient(clientConnection)
	s.stopClient = clientConnection.Close

	server := sigverification.NewVerifierServer(s.parallelExecutionConfig, verificationScheme)
	go func() {
		utils.RunServerMain(&serverConnectionConfig, func(grpcServer *grpc.Server) {
			s.stopServer = grpcServer.GracefulStop
			sigverification.RegisterVerifierServer(grpcServer, server)
		})
	}()

	s.signer = sigverification.NewTxSigner(verificationScheme)
}

func (s *testState) tearDown() {
	err := s.stopClient()
	if err != nil {
		log.Fatalf("failed to close connection: %v", err)
	}
	s.stopServer()
}

var parallelExecutionConfig = &sigverification.ParallelExecutionConfig{
	BatchSizeCutoff:   3,
	BatchTimeCutoff:   1 * time.Hour,
	Parallelism:       3,
	ChannelBufferSize: 1,
}

func TestNoVerificationKeySet(t *testing.T) {
	c := &testState{testing: t, parallelExecutionConfig: parallelExecutionConfig}
	c.setUp()

	stream, err := c.client.StartStream(context.Background())
	Expect(err).To(BeNil())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	_, err = stream.Recv()
	Expect(err).NotTo(BeNil())

	c.tearDown()
}

func TestNoInput(t *testing.T) {
	c := &testState{testing: t, parallelExecutionConfig: parallelExecutionConfig}
	c.setUp()

	verificationKey, _ := c.signer.NewKeys()

	_, err := c.client.SetVerificationKey(context.Background(), verificationKey)
	Expect(err).To(BeNil())

	stream, _ := c.client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	output := channel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(testTimeout).ShouldNot(Receive())

	c.tearDown()
}

func TestMinimalInput(t *testing.T) {
	c := &testState{testing: t, parallelExecutionConfig: parallelExecutionConfig}
	c.setUp()

	verificationKey, signingKey := c.signer.NewKeys()

	_, err := c.client.SetVerificationKey(context.Background(), verificationKey)
	Expect(err).To(BeNil())

	stream, _ := c.client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: c.signedTx([][]byte{}, signingKey)},
		{BlockNum: 1, TxNum: 2, Tx: c.signedTx([][]byte{}, signingKey)},
		{BlockNum: 1, TxNum: 3, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}}},
	}})
	Expect(err).To(BeNil())

	output := channel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(1 * time.Second).Should(Receive(batchWithResponses(HaveLen(3))))

	c.tearDown()
}

func (t *testState) signedTx(serialNumbers []sigverification.SerialNumber, signingKey *sigverification.Key) *token.Tx {
	signature, err := t.signer.SignTx(signingKey, serialNumbers)
	Expect(err).To(BeNil())
	return &token.Tx{SerialNumbers: serialNumbers, Signature: signature}
}

func batchWithResponses(matcher types.GomegaMatcher) types.GomegaMatcher {
	return Satisfy(func(response *sigverification.ResponseBatch) bool {
		match, _ := matcher.Match(response.Responses)
		return match
	})
}

func channel(stream sigverification.Verifier_StartStreamClient) <-chan *sigverification.ResponseBatch {
	output := make(chan *sigverification.ResponseBatch)
	go func() {
		for {
			response, _ := stream.Recv()
			output <- response
		}
	}()
	return output
}
