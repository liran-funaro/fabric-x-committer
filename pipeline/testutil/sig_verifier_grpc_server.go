package testutil

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"google.golang.org/grpc"
)

var DefaultSigVerifierBehavior = func(requestBatch *sigverification.RequestBatch) *sigverification.ResponseBatch {
	responseBatch := &sigverification.ResponseBatch{}
	for _, request := range requestBatch.Requests {
		responseBatch.Responses = append(responseBatch.Responses, &sigverification.Response{
			BlockNum: request.BlockNum,
			TxNum:    request.TxNum,
			IsValid:  true,
		})
	}
	return responseBatch
}

type SigVerifierGrpcServer struct {
	t          *testing.T
	grpcServer *grpc.Server
}

func NewSigVerifierGrpcServer(
	t *testing.T,
	behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch,
	port int,
) *SigVerifierGrpcServer {

	grpcServer := grpc.NewServer()
	sigverification.RegisterVerifierServer(grpcServer,
		&sigVerifierImpl{
			t:        t,
			behavior: behavior,
		},
	)

	startGrpcServer(t, port, grpcServer)

	return &SigVerifierGrpcServer{
		t:          t,
		grpcServer: grpcServer,
	}
}

func (s *SigVerifierGrpcServer) Stop() {
	s.grpcServer.GracefulStop()
}

type sigVerifierImpl struct {
	t *testing.T
	sigverification.UnimplementedVerifierServer
	behavior func(reqBatch *sigverification.RequestBatch) *sigverification.ResponseBatch
}

func (s *sigVerifierImpl) SetVerificationKey(context context.Context, key *sigverification.Key) (*sigverification.Empty, error) {
	return nil, nil
}

func (s *sigVerifierImpl) StartStream(stream sigverification.Verifier_StartStreamServer) error {
	for {
		requestBatch, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			require.NoError(s.t, err)
		}
		responseBatch := s.behavior(requestBatch)
		require.NoError(s.t, stream.Send(responseBatch))
	}
}

func startGrpcServer(t *testing.T, port int, grpcServer *grpc.Server) {
	//bufconn.Listen(1024 * 1024)
	address := fmt.Sprintf("localhost:%d", port)
	go func() {
		lis, err := net.Listen("tcp", address)
		require.NoError(t, err)
		require.NoError(t, grpcServer.Serve(lis))
	}()

	for i := 0; i < 10; i++ {
		conn, err := net.DialTimeout("tcp", address, 1*time.Second)
		if err != nil {
			if i == 9 {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		require.NoError(t, conn.Close())
		break
	}
}
