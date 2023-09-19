package coordinatorservice

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

type coordinatorTestEnv struct {
	coordinator *CoordinatorService
	csStream    protocoordinatorservice.Coordinator_BlockProcessingClient
}

func newCoordinatorTestEnv(t *testing.T) *coordinatorTestEnv {
	svServerConfigs, svServices, svGrpcServers := sigverifiermock.StartMockSVService(3)
	vcServerConfigs, vcServices, vcGrpcServers := vcservicemock.StartMockVCService(3)

	c := &CoordinatorConfig{
		SignVerifierConfig: &SignVerifierConfig{
			ServerConfig: svServerConfigs,
		},
		DependencyGraphConfig: &DependencyGraphConfig{
			NumOfLocalDepConstructors:       3,
			WaitingTxsLimit:                 2000,
			NumOfWorkersForGlobalDepManager: 3,
		},
		ValidatorCommitterConfig: &ValidatorCommitterConfig{
			ServerConfig: vcServerConfigs,
		},
		ChannelBufferSizePerGoroutine: 2000,
	}

	cs := NewCoordinatorService(c)

	signErrChan, valErrChan, err := cs.Start()
	require.NoError(t, err)

	var wgSignErrChan sync.WaitGroup
	wgSignErrChan.Add(1)
	go func() {
		errS := <-signErrChan
		require.NoError(t, errS)
		wgSignErrChan.Done()
	}()

	var wgValErrChan sync.WaitGroup
	wgValErrChan.Add(1)
	go func() {
		errV := <-valErrChan
		require.NoError(t, errV)
		wgValErrChan.Done()
	}()

	sc := &connection.ServerConfig{
		Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: 0,
		},
	}
	var grpcSrv *grpc.Server

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		connection.RunServerMain(sc, func(grpcServer *grpc.Server, actualListeningPort int) {
			grpcSrv = grpcServer
			sc.Endpoint.Port = actualListeningPort
			protocoordinatorservice.RegisterCoordinatorServer(grpcServer, cs)
			wg.Done()
		})
	}()
	wg.Wait()

	conn, err := connection.Connect(connection.NewDialConfig(sc.Endpoint))
	require.NoError(t, err)

	client := protocoordinatorservice.NewCoordinatorClient(conn)
	csStream, err := client.BlockProcessing(context.Background())
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, conn.Close())

		require.NoError(t, cs.Close())

		require.NoError(t, csStream.CloseSend())

		for _, mockSV := range svServices {
			mockSV.Close()
		}
		wgSignErrChan.Wait()

		for _, mockVC := range vcServices {
			mockVC.Close()
		}
		wgValErrChan.Wait()

		for _, s := range svGrpcServers {
			s.Stop()
		}

		for _, s := range vcGrpcServers {
			s.Stop()
		}

		grpcSrv.Stop()
	})

	return &coordinatorTestEnv{
		coordinator: cs,
		csStream:    csStream,
	}
}

func TestCoordinatorService(t *testing.T) {
	env := newCoordinatorTestEnv(t)

	t.Run("valid tx", func(t *testing.T) {
		err := env.csStream.Send(&protoblocktx.Block{
			Number: 0,
			Txs:    []*protoblocktx.Tx{{Id: "tx1"}},
		})
		require.NoError(t, err)

		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		expectedTxStatus := &protocoordinatorservice.TxValidationStatusBatch{
			TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
				{
					TxId:   "tx1",
					Status: protoblocktx.Status_COMMITTED,
				},
			},
		}
		require.Equal(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)
	})

	t.Run("invalid signature", func(t *testing.T) {
		err := env.csStream.Send(&protoblocktx.Block{
			Number: 1,
			Txs:    []*protoblocktx.Tx{{Id: "tx2"}},
		})
		require.NoError(t, err)

		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		expectedTxStatus := &protocoordinatorservice.TxValidationStatusBatch{
			TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
				{
					TxId:   "tx2",
					Status: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
				},
			},
		}
		require.Equal(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)
	})

	t.Run("out of order block", func(t *testing.T) {
		// next expected block is 2, but sending 4 to 510
		lastBlockNum := 600
		for i := 4; i <= lastBlockNum; i++ {
			err := env.csStream.Send(&protoblocktx.Block{
				Number: uint64(i),
				Txs:    []*protoblocktx.Tx{{Id: "tx" + strconv.Itoa(i)}},
			})
			require.NoError(t, err)
		}

		// send block 2 which is the next expected block
		env.coordinator.queues.blockWithValidSignTxs <- &protoblocktx.Block{
			Number: 2,
			Txs:    []*protoblocktx.Tx{{Id: "tx2"}},
		}

		// send block 3 which is the next expected block
		env.coordinator.queues.blockWithValidSignTxs <- &protoblocktx.Block{
			Number: 3,
			Txs:    []*protoblocktx.Tx{},
		}
		env.coordinator.queues.blockWithInvalidSignTxs <- &protoblocktx.Block{
			Number: 3,
			Txs:    []*protoblocktx.Tx{{Id: "tx3"}},
		}

		numValid := 0
		numInvalid := 0
		for i := 2; i <= lastBlockNum; i++ {
			txStatus, err := env.csStream.Recv()
			require.NoError(t, err)
			if txStatus.TxsValidationStatus[0].Status != protoblocktx.Status_COMMITTED {
				numInvalid++
			} else {
				numValid++
			}
		}
		require.Equal(t, lastBlockNum/2, numValid)
		require.Equal(t, lastBlockNum/2-1, numInvalid)
	})
}
