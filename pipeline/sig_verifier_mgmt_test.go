package pipeline

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"google.golang.org/grpc"
)

func TestSigVerifiersMgr(t *testing.T) {
	s := &testSigVerifierServer{t: t}
	s.start()
	defer s.stop()

	m, err := newSigVerificationMgr(
		&SigVerifierMgrConfig{
			SigVerifierServers: []string{"localhost"},
			BatchCutConfig: &SigVerifiedBatchConfig{
				BatchSize:     2,
				TimeoutMillis: 20000,
			},
		},
	)
	assert.NoError(t, err)
	defer m.stop()

	g := &testBlockGenerator{}
	b := g.generateNextBlock(2)
	m.processBlockAsync(b)

	txSeqNums := <-m.outputChanValids
	require.Equal(t, 2, len(txSeqNums))
	require.Equal(t, txSeqNum{blkNum: 0, txNum: 0}, txSeqNums[0])
	require.Equal(t, txSeqNum{blkNum: 0, txNum: 1}, txSeqNums[1])
}

type testSigVerifierServer struct {
	t          *testing.T
	grpcServer *grpc.Server

	sigverification.UnimplementedVerifierServer
}

func (s *testSigVerifierServer) start() {
	address := fmt.Sprintf("localhost:%d", config.GRPC_PORT)
	s.grpcServer = grpc.NewServer()
	go func() {

		sigverification.RegisterVerifierServer(s.grpcServer, s)
		lis, err := net.Listen("tcp", address)
		require.NoError(s.t, err)
		require.NoError(s.t, s.grpcServer.Serve(lis))
	}()

	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", address, 1*time.Second)
		if err != nil {
			if i == 9 {
				s.t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		require.NoError(s.t, conn.Close())
		break
	}
}

func (s *testSigVerifierServer) stop() {
	s.grpcServer.GracefulStop()
}

func (s *testSigVerifierServer) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		responseBatch := &sigverification.ResponseBatch{}

		for _, request := range requestBatch.Requests {
			responseBatch.Responses = append(responseBatch.Responses, &sigverification.Response{
				BlockNum: request.BlockNum,
				TxNum:    request.TxNum,
				IsValid:  true,
			})
		}
		require.NoError(s.t, stream.Send(responseBatch))
	}
}

func (s *testSigVerifierServer) SetVerificationKey(context context.Context, key *sigverification.Key) (*sigverification.Empty, error) {
	return nil, nil
}

type testBlockGenerator struct {
	nextBlockNum uint64
}

func (g *testBlockGenerator) generateNextBlock(numTx int) *token.Block {
	b := &token.Block{
		Number: g.nextBlockNum,
	}
	for i := 0; i < numTx; i++ {
		t := &token.Tx{
			SerialNumbers: [][]byte{[]byte("Junk Serial Number")},
		}
		b.Txs = append(b.Txs, t)
	}
	return b
}
