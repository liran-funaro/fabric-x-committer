package coordinatorservice

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

type coordinatorTestEnv struct {
	coordinator *CoordinatorService
	config      *CoordinatorConfig
	grpcServer  *grpc.Server
	client      protocoordinatorservice.CoordinatorClient
	csStream    protocoordinatorservice.Coordinator_BlockProcessingClient
}

func newCoordinatorTestEnv(t *testing.T, numSigService, numVcService int) *coordinatorTestEnv {
	svServerConfigs, svServices, svGrpcServers := sigverifiermock.StartMockSVService(numSigService)
	t.Cleanup(func() {
		for _, mockSV := range svServices {
			mockSV.Close()
		}
		for _, s := range svGrpcServers {
			s.Stop()
		}
	})
	vcServerConfigs, vcServices, vcGrpcServers := vcservicemock.StartMockVCService(numVcService)
	t.Cleanup(func() {
		for _, mockVC := range vcServices {
			mockVC.Close()
		}
		for _, s := range vcGrpcServers {
			s.Stop()
		}
	})

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

	return &coordinatorTestEnv{
		coordinator: NewCoordinatorService(c),
		config:      c,
	}
}

func (e *coordinatorTestEnv) start(ctx context.Context, t *testing.T) {
	dCtx, cancel := context.WithCancel(ctx)
	cs := e.coordinator
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() { require.NoError(t, connection.FilterStreamErrors(cs.Run(dCtx))); wg.Done() }()

	sc := &connection.ServerConfig{
		Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: 0,
		},
	}
	var grpcSrv *grpc.Server

	var grpcSrvG sync.WaitGroup
	grpcSrvG.Add(1)
	go func() {
		connection.RunServerMain(sc, func(grpcServer *grpc.Server, actualListeningPort int) {
			grpcSrv = grpcServer
			sc.Endpoint.Port = actualListeningPort
			protocoordinatorservice.RegisterCoordinatorServer(grpcServer, cs)
			grpcSrvG.Done()
		})
	}()
	grpcSrvG.Wait()
	t.Cleanup(grpcSrv.Stop)

	e.grpcServer = grpcSrv
	conn, err := connection.Connect(connection.NewDialConfig(sc.Endpoint))
	require.NoError(t, err)

	client := protocoordinatorservice.NewCoordinatorClient(conn)
	e.client = client

	sCtx, sCancel := context.WithTimeout(ctx, 2*time.Minute)
	t.Cleanup(sCancel)
	csStream, err := client.BlockProcessing(sCtx)
	require.NoError(t, err)

	e.csStream = csStream

	t.Cleanup(cancel)
}

func TestCoordinatorOneActiveStreamOnly(t *testing.T) {
	env := newCoordinatorTestEnv(t, 1, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	require.Eventually(t, func() bool {
		return env.coordinator.isStreamActive.Load()
	}, 4*time.Second, 250*time.Millisecond)

	stream, err := env.client.BlockProcessing(ctx)
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, utils.ErrActiveStream.Error())
}

func TestCoordinatorServiceValidTx(t *testing.T) {
	env := newCoordinatorTestEnv(t, 2, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	p := &protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	}
	pBytes, err := proto.Marshal(p)
	require.NoError(t, err)
	err = env.csStream.Send(&protoblocktx.Block{
		Number: 0,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      1,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("key"),
							},
						},
					},
					{
						NsId:      uint32(types.MetaNamespaceID),
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   types.NamespaceID(2).Bytes(),
								Value: pBytes,
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
					[]byte("dummy"),
				},
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

	_, err = env.coordinator.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 0})
	require.NoError(t, err)

	lastBlock, err := env.coordinator.GetLastCommittedBlockNumber(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastBlock.Number)
}

func TestCoordinatorServiceOutofOrderBlock(t *testing.T) {
	env := newCoordinatorTestEnv(t, 2, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)
	// next expected block is 0, but sending 2 to 500
	lastBlockNum := 500
	for i := 2; i <= lastBlockNum; i++ {
		err := env.csStream.Send(&protoblocktx.Block{
			Number: uint64(i),
			Txs: []*protoblocktx.Tx{
				{
					Id: "tx" + strconv.Itoa(i),
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: types.VersionNumber(0).Bytes(),
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key: []byte("key"),
								},
							},
						},
					},
					Signatures: [][]byte{[]byte("dummy")},
				},
			},
		})
		require.NoError(t, err)
	}

	require.Never(t, func() bool {
		return test.GetMetricValue(
			t,
			env.coordinator.metrics.transactionCommittedStatusSentTotal,
		) > 10
	}, 2*time.Second, 100*time.Millisecond)

	// send block 0 which is the next expected block but an empty block
	err := env.csStream.Send(&protoblocktx.Block{
		Number: uint64(0),
		Txs:    []*protoblocktx.Tx{},
	})
	require.NoError(t, err)

	// send block 1 which is the next expected block
	env.coordinator.queues.blockWithValidSignTxs <- &protoblocktx.Block{
		Number: 1,
		Txs:    []*protoblocktx.Tx{},
	}
	env.coordinator.queues.blockWithInvalidSignTxs <- &protoblocktx.Block{
		Number: 1,
		Txs:    []*protoblocktx.Tx{{Id: "tx3"}},
	}

	numValid := 0
	numInvalid := 0
	for i := 1; i <= lastBlockNum; i++ {
		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		if txStatus.TxsValidationStatus[0].Status != protoblocktx.Status_COMMITTED {
			numInvalid++
		} else {
			numValid++
		}
	}
	require.Equal(t, lastBlockNum-1, numValid)
	require.Equal(t, 1, numInvalid)

	require.Equal(
		t,
		float64(lastBlockNum-1), // block 2 to block 600 + old 1 blocks as block 2 is empty
		test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
	)
	require.Equal(
		t,
		float64(1),
		test.GetMetricValue(t, env.coordinator.metrics.transactionInvalidSignatureStatusSentTotal),
	)
}

func TestQueueSize(t *testing.T) { // nolint:gocognit
	env := newCoordinatorTestEnv(t, 2, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	go env.coordinator.monitorQueues(ctx)

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

func TestCoordinatorRecovery(t *testing.T) {
	env := newCoordinatorTestEnv(t, 1, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	require.Equal(t, uint64(0), env.coordinator.firstExpectedBlockNumber)
	require.Equal(t, uint64(0), env.coordinator.postRecoveryStartBlockNumber)

	err := env.csStream.Send(&protoblocktx.Block{
		Number: 0,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      1,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   []byte("key1"),
								Value: []byte("value1"),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
		},
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
	_, err = env.client.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 0})
	require.NoError(t, err)

	lastCommittedBlock, err := env.client.GetLastCommittedBlockNumber(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(0), lastCommittedBlock.Number)

	// To simulate a failure scenario in which a block is partially committed, we first create block 1
	// with single transaction but actual block 1 is supposed to have 2 transactions. Once the partial block 1
	// is committed, we will restart the service and send a full block 1 with 2 transactions.
	block1 := &protoblocktx.Block{
		Number: 1,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      1,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("key2"),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
		},
	}
	require.NoError(t, env.csStream.Send(block1))

	txStatus, err = env.csStream.Recv()
	require.NoError(t, err)
	expectedTxStatus = &protocoordinatorservice.TxValidationStatusBatch{
		TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
			{
				TxId:   "tx2",
				Status: protoblocktx.Status_COMMITTED,
			},
		},
	}
	require.Equal(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)

	require.Eventually(t, func() bool {
		lastSeenBlock, lastSeenErr := env.coordinator.validatorCommitterMgr.getMaxSeenBlockNumber(ctx)
		if lastSeenErr != nil {
			return false
		}
		return uint64(1) == lastSeenBlock.Number
	}, 2*time.Second, 250*time.Millisecond)

	require.Equal(t, uint64(0), env.coordinator.firstExpectedBlockNumber)
	require.Equal(t, uint64(0), env.coordinator.postRecoveryStartBlockNumber)

	cancel()
	env.grpcServer.Stop()

	ctx, cancel = context.WithCancel(context.Background())
	t.Cleanup(cancel)
	env.coordinator = NewCoordinatorService(env.config)
	env.start(ctx, t)

	require.Eventually(t, func() bool {
		return uint64(1) == env.coordinator.firstExpectedBlockNumber
	}, 4*time.Second, 250*time.Millisecond)
	require.Equal(t, uint64(2), env.coordinator.postRecoveryStartBlockNumber)

	// Now, we are sending the full block 1. As tx2 is already committed, we get the status of
	// tx2 first. Then, tx3 will be validated and committed.
	block1 = &protoblocktx.Block{
		Number: 1,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      1,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("key2"),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
			{
				Id: "tx3",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      1,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("key3"),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
		},
	}

	require.NoError(t, env.csStream.Send(block1))

	tx2Status, err := env.csStream.Recv()
	require.NoError(t, err)
	expectedTxStatus = &protocoordinatorservice.TxValidationStatusBatch{
		TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
			{
				TxId:   "tx2",
				Status: protoblocktx.Status_COMMITTED,
			},
		},
	}
	require.Equal(t, expectedTxStatus.TxsValidationStatus, tx2Status.TxsValidationStatus)
	tx3Status, err := env.csStream.Recv()
	require.NoError(t, err)
	expectedTxStatus = &protocoordinatorservice.TxValidationStatusBatch{
		TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
			{
				TxId:   "tx3",
				Status: protoblocktx.Status_COMMITTED,
			},
		},
	}
	require.Equal(t, expectedTxStatus.TxsValidationStatus, tx3Status.TxsValidationStatus)

	_, err = env.client.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 1})
	require.NoError(t, err)

	require.Equal(t, uint64(1), env.coordinator.firstExpectedBlockNumber)
	require.Equal(t, uint64(2), env.coordinator.postRecoveryStartBlockNumber)
}

func TestGRPCConnectionFailure(t *testing.T) {
	logging.SetupWithConfig(&logging.Config{
		Enabled: true,
		Level:   logging.Debug,
	})
	c := NewCoordinatorService(&CoordinatorConfig{
		ServerConfig: &connection.ServerConfig{
			Endpoint: connection.Endpoint{Host: "", Port: 1876},
		},
		SignVerifierConfig: &SignVerifierConfig{
			ServerConfig: []*connection.ServerConfig{{Endpoint: connection.Endpoint{Host: "random", Port: 1234}}},
		},
		DependencyGraphConfig: &DependencyGraphConfig{},
		ValidatorCommitterConfig: &ValidatorCommitterConfig{
			ServerConfig: []*connection.ServerConfig{{Endpoint: connection.Endpoint{Host: "random", Port: 1234}}},
		},
		Monitoring: &monitoring.Config{
			Metrics: &metrics.Config{
				Endpoint: &connection.Endpoint{Host: "", Port: 1877},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := c.Run(ctx)
	require.ErrorContains(t, err, "transport: Error while dialing: dial tcp: lookup random")
	require.ErrorContains(t, err, "no such host")
}
