package coordinatorservice

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
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
		Monitoring: &monitoring.Config{
			Metrics: &metrics.Config{
				Enable: true,
				Endpoint: &connection.Endpoint{
					Host: "localhost",
					Port: 0,
				},
			},
		},
	}

	cs := NewCoordinatorService(c)

	t.Cleanup(func() {
		for _, mockSV := range svServices {
			mockSV.Close()
		}

		for _, mockVC := range vcServices {
			mockVC.Close()
		}

		for _, s := range svGrpcServers {
			s.Stop()
		}

		for _, s := range vcGrpcServers {
			s.Stop()
		}
	})

	return &coordinatorTestEnv{
		coordinator: cs,
	}
}

func (e *coordinatorTestEnv) start(t *testing.T) {
	cs := e.coordinator
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

	e.csStream = csStream

	t.Cleanup(func() {
		require.NoError(t, conn.Close())

		require.NoError(t, cs.Close())

		require.NoError(t, csStream.CloseSend())

		wgSignErrChan.Wait()

		wgValErrChan.Wait()

		grpcSrv.Stop()
	})
}

func TestCoordinatorService(t *testing.T) {
	env := newCoordinatorTestEnv(t)
	env.start(t)

	t.Run("valid tx", func(t *testing.T) {
		err := env.csStream.Send(&protoblocktx.Block{
			Number: 0,
			Txs: []*protoblocktx.Tx{
				{
					Id:        "tx1",
					Signature: []byte("dummy"),
				},
			},
		})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal) == 1
		}, 1*time.Second, 100*time.Millisecond)

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
		require.Equal(
			t,
			float64(1),
			test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
		)
	})

	t.Run("invalid signature", func(t *testing.T) {
		err := env.csStream.Send(&protoblocktx.Block{
			Number: 1,
			Txs:    []*protoblocktx.Tx{{Id: "tx2"}},
		})
		require.NoError(t, err)
		require.Eventually(t, func() bool {
			return test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal) == 2
		}, 1*time.Second, 100*time.Millisecond)

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
		require.Equal(
			t,
			float64(1),
			test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
		)
	})

	t.Run("out of order block", func(t *testing.T) {
		// next expected block is 2, but sending 4 to 600
		lastBlockNum := 600
		for i := 4; i <= lastBlockNum; i++ {
			err := env.csStream.Send(&protoblocktx.Block{
				Number: uint64(i),
				Txs: []*protoblocktx.Tx{
					{
						Id:        "tx" + strconv.Itoa(i),
						Signature: []byte("dummy"),
					},
				},
			})
			require.NoError(t, err)
		}

		require.Never(t, func() bool {
			return test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal) > 10
		}, 5*time.Second, 100*time.Millisecond)

		// send block 2 which is the next expected block but an empty block
		err := env.csStream.Send(&protoblocktx.Block{
			Number: uint64(2),
			Txs:    []*protoblocktx.Tx{},
		})
		require.NoError(t, err)

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
		for i := 3; i <= lastBlockNum; i++ {
			txStatus, err := env.csStream.Recv()
			require.NoError(t, err)
			if txStatus.TxsValidationStatus[0].Status != protoblocktx.Status_COMMITTED {
				numInvalid++
			} else {
				numValid++
			}
		}
		require.Equal(t, lastBlockNum-3, numValid)
		require.Equal(t, 1, numInvalid)

		require.Equal(
			t,
			float64(lastBlockNum-2), // block 4 to block 600 + old 1 blocks as block 2 is empty
			test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
		)
		require.Equal(
			t,
			float64(2),
			test.GetMetricValue(t, env.coordinator.metrics.transactionInvalidSignatureStatusSentTotal),
		)
	})
}

func TestQueueSize(t *testing.T) { // nolint:gocognit
	env := newCoordinatorTestEnv(t)
	env.coordinator.promErrChan = make(chan error)
	go env.coordinator.monitorQueues()

	q := env.coordinator.queues
	m := env.coordinator.metrics
	q.blockForSignatureVerification <- &protoblocktx.Block{}
	q.blockWithValidSignTxs <- &protoblocktx.Block{}
	q.blockWithInvalidSignTxs <- &protoblocktx.Block{}
	q.txsBatchForDependencyGraph <- &dependencygraph.TransactionBatch{}
	q.dependencyFreeTxsNode <- []*dependencygraph.TransactionNode{}
	q.validatedTxsNode <- []*dependencygraph.TransactionNode{}
	q.txsStatus <- &protovcservice.TransactionStatus{}

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, m.sigverifierInputBlockQueueSize) == 1 &&
			test.GetMetricValue(t, m.sigverifierOutputValidBlockQueueSize) == 1 &&
			test.GetMetricValue(t, m.sigverifierOutputInvalidBlockQueueSize) == 1 &&
			test.GetMetricValue(t, m.vcserviceInputTxBatchQueueSize) == 1 &&
			test.GetMetricValue(t, m.vcserviceOutputValidatedTxBatchQueueSize) == 1 &&
			test.GetMetricValue(t, m.vcserviceOutputTxStatusBatchQueueSize) == 1
	}, 3*time.Second, 500*time.Millisecond)

	<-q.blockForSignatureVerification
	<-q.blockWithValidSignTxs
	<-q.blockWithInvalidSignTxs
	<-q.txsBatchForDependencyGraph
	<-q.dependencyFreeTxsNode
	<-q.validatedTxsNode
	<-q.txsStatus

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, m.sigverifierInputBlockQueueSize) == 0 &&
			test.GetMetricValue(t, m.sigverifierOutputValidBlockQueueSize) == 0 &&
			test.GetMetricValue(t, m.sigverifierOutputInvalidBlockQueueSize) == 0 &&
			test.GetMetricValue(t, m.vcserviceInputTxBatchQueueSize) == 0 &&
			test.GetMetricValue(t, m.vcserviceOutputValidatedTxBatchQueueSize) == 0 &&
			test.GetMetricValue(t, m.vcserviceOutputTxStatusBatchQueueSize) == 0
	}, 3*time.Second, 500*time.Millisecond)
}
