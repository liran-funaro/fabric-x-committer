package sigverification_test

import (
	"context"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/grpc"
)

// De facto constants.
var (
	SerialNumberCountDistribution = test.Constant(int64(test.TxSize))
	SerialNumberSize              = test.Constant(32)
	OutputCountDistribution       = test.Constant(int64(test.TxSize))
	OutputSize                    = test.Constant(32)
	SignatureValidRatio           = test.Always
	BatchSizeDistribution         = test.Constant(int64(test.BatchSize))
	VerificationScheme            = signature.Ecdsa
)

// TypicalTxValidationDelay Typical result values from experiments.
var TypicalTxValidationDelay = int64(time.Second / 10_000)

// Optimal values resulting from benchmarking.
var (
	OptimalBatchTimeCutoff   = 500 * time.Millisecond
	OptimalChannelBufferSize = 50
)

// State test state.
type State struct {
	Client sigverification.VerifierClient
}

func NewTestState(t test.TestingT, server sigverification.VerifierServer) *State {
	serverConnectionConfig := connection.ServerConfig{Endpoint: connection.Endpoint{Host: "localhost", Port: 0}}
	test.RunGrpcServerForTest(context.Background(), t, &serverConnectionConfig, func(grpcServer *grpc.Server) {
		sigverification.RegisterVerifierServer(grpcServer, server)
	})

	clientConnectionConfig := connection.NewDialConfig(&serverConnectionConfig.Endpoint)
	clientConnection, err := connection.Connect(clientConnectionConfig)
	require.NoError(t, err)
	t.Cleanup(func() {
		assert.NoError(t, clientConnection.Close())
	})
	return &State{
		Client: sigverification.NewVerifierClient(clientConnection),
	}
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
