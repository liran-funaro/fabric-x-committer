package coordinatorservice

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
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
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

type (
	coordinatorTestEnv struct {
		coordinator *CoordinatorService
		config      *CoordinatorConfig
		grpcServer  *grpc.Server
		client      protocoordinatorservice.CoordinatorClient
		csStream    protocoordinatorservice.Coordinator_BlockProcessingClient
		dbEnv       *vcservice.DatabaseTestEnv
	}

	testConfig struct {
		numSigService int
		numVcService  int
		mockVcService bool
	}
)

func newCoordinatorTestEnv(ctx context.Context, t *testing.T, tConfig *testConfig) *coordinatorTestEnv {
	svServerConfigs, svServices, svGrpcServers := sigverifiermock.StartMockSVService(tConfig.numSigService)
	t.Cleanup(func() {
		for _, mockSV := range svServices {
			mockSV.Close()
		}
		for _, s := range svGrpcServers {
			s.Stop()
		}
	})

	vcServerConfigs := make([]*connection.ServerConfig, 0, tConfig.numVcService)
	var vcsTestEnv *vcservice.ValidatorAndCommitterServiceTestEnv

	if !tConfig.mockVcService {
		vcsTestEnv = vcservice.NewValidatorAndCommitServiceTestEnv(ctx, t)
		vcServerConfigs = append(vcServerConfigs, vcsTestEnv.Config.Server)

		for range tConfig.numVcService - 1 {
			vcs := vcservice.NewValidatorAndCommitServiceTestEnv(ctx, t, vcsTestEnv.GetDBEnv(t))
			vcServerConfigs = append(vcServerConfigs, vcs.Config.Server)
		}
	} else {
		serverConfig, vcServices, vcGrpcServers := vcservicemock.StartMockVCService(tConfig.numVcService)
		t.Cleanup(func() {
			for _, mockVC := range vcServices {
				mockVC.Close()
			}
			for _, s := range vcGrpcServers {
				s.Stop()
			}
		})

		vcServerConfigs = serverConfig
	}

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
		dbEnv:       vcsTestEnv.GetDBEnv(t),
	}
}

func (e *coordinatorTestEnv) start(ctx context.Context, t *testing.T) {
	dCtx, cancel := context.WithCancel(ctx)
	cs := e.coordinator
	var wg sync.WaitGroup
	t.Cleanup(wg.Wait)
	wg.Add(1)
	go func() { require.NoError(t, connection.FilterStreamErrors(cs.Run(dCtx))); wg.Done() }()

	ctxConnWait, cancelConnWait := context.WithTimeout(dCtx, 2*time.Minute)
	defer cancelConnWait()
	cs.WaitForReady(ctxConnWait)

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

func (e *coordinatorTestEnv) createNamespace(t *testing.T, blkNum, nsID int) {
	p := &protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	}
	pBytes, err := proto.Marshal(p)
	require.NoError(t, err)

	txID := uuid.NewString()
	err = e.csStream.Send(&protoblocktx.Block{
		Number: uint64(blkNum), // nolint:gosec
		Txs: []*protoblocktx.Tx{
			{
				Id: txID,
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      uint32(types.MetaNamespaceID),
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   types.NamespaceID(nsID).Bytes(), // nolint:gosec
								Value: pBytes,
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
		},
		TxsNum: []uint32{0},
	})
	require.NoError(t, err)
	txStatus, err := e.csStream.Recv()
	require.NoError(t, err)
	expectedTxStatus := &protocoordinatorservice.TxValidationStatusBatch{
		TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
			{
				TxId:   txID,
				Status: protoblocktx.Status_COMMITTED,
			},
		},
	}
	require.Equal(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)
}

func TestCoordinatorOneActiveStreamOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env := newCoordinatorTestEnv(ctx, t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: true})
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env := newCoordinatorTestEnv(ctx, t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: false})
	env.start(ctx, t)

	env.createNamespace(t, 0, 1)

	p := &protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	}
	pBytes, err := proto.Marshal(p)
	require.NoError(t, err)
	err = env.csStream.Send(&protoblocktx.Block{
		Number: 1,
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
		TxsNum: []uint32{0},
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
				TxId:   "tx1",
				Status: protoblocktx.Status_COMMITTED,
			},
		},
	}
	require.Equal(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)
	require.Equal(
		t,
		float64(2),
		test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
	)

	_, err = env.coordinator.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 1})
	require.NoError(t, err)

	lastBlock, err := env.coordinator.GetLastCommittedBlockNumber(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), lastBlock.Number)
}

func TestCoordinatorServiceOutofOrderBlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env := newCoordinatorTestEnv(ctx, t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: true})
	env.start(ctx, t)

	require.Never(t, func() bool {
		return env.coordinator.nextExpectedBlockNumberToBeReceived > 0
	}, 3*time.Second, 1*time.Second)
	require.Equal(t, uint64(0), env.coordinator.nextExpectedBlockNumberToBeReceived)

	// next expected block is 0, but sending 2 to 500
	env.coordinator.nextExpectedBlockNumberToBeReceived = 2
	lastBlockNum := 500
	for i := 2; i <= lastBlockNum; i++ {
		err := env.csStream.Send(&protoblocktx.Block{
			Number: uint64(i), // nolint:gosec
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
			TxsNum: []uint32{0},
		})
		require.NoError(t, err)
	}

	// as the nextBlockNumberForDepGraph is 0, none of the transactions would get committed.
	require.Never(t, func() bool {
		return test.GetMetricValue(
			t,
			env.coordinator.metrics.transactionCommittedStatusSentTotal,
		) > 10
	}, 4*time.Second, 100*time.Millisecond)

	// send block 0 which is the original next expected block.
	env.coordinator.nextExpectedBlockNumberToBeReceived = 0
	err := env.csStream.Send(&protoblocktx.Block{
		Number: uint64(0),
		Txs:    []*protoblocktx.Tx{},
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return env.coordinator.nextExpectedBlockNumberToBeReceived == 1
	}, 3*time.Second, 250*time.Millisecond)
	// send block 1 which is the next expected block
	env.coordinator.queues.blockWithValidSignTxs <- &protoblocktx.Block{
		Number: 1,
		Txs:    []*protoblocktx.Tx{},
	}
	env.coordinator.queues.blockWithInvalidSignTxs <- &protoblocktx.Block{
		Number: 1,
		Txs:    []*protoblocktx.Tx{{Id: "tx3"}},
		TxsNum: []uint32{0},
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

	actualCommittedTxCount := test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal)
	require.Equal(t, float64(lastBlockNum-1), actualCommittedTxCount)
	actualInvSignTxCount := test.GetMetricValue(t, env.coordinator.metrics.transactionInvalidSignatureStatusSentTotal)
	require.Equal(t, float64(1), actualInvSignTxCount)
}

func TestQueueSize(t *testing.T) { // nolint:gocognit
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env := newCoordinatorTestEnv(ctx, t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: true})
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env := newCoordinatorTestEnv(ctx, t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: false})
	env.start(ctx, t)

	require.Equal(t, uint64(0), env.coordinator.nextExpectedBlockNumberToBeReceived)
	env.createNamespace(t, 0, 1)
	require.Equal(t, uint64(1), env.coordinator.nextExpectedBlockNumberToBeReceived)

	err := env.csStream.Send(&protoblocktx.Block{
		Number: 1,
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
		TxsNum: []uint32{0},
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
	_, err = env.client.SetLastCommittedBlockNumber(ctx, &protoblocktx.BlockInfo{Number: 1})
	require.NoError(t, err)

	lastCommittedBlock, err := env.client.GetLastCommittedBlockNumber(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, uint64(1), lastCommittedBlock.Number)

	// To simulate a failure scenario in which a block is partially committed, we first create block 2
	// with two transaction but actual block 2 is supposed to have four transactions. Once the partial block 2
	// is committed, we will restart the service and send a full block 2 with all four transactions.
	nsPolicy, err := proto.Marshal(&protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	})
	require.NoError(t, err)
	block2 := &protoblocktx.Block{
		Number: 2,
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
				Id: "mvcc conflict",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      2,
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
		TxsNum: []uint32{0, 2, 5},
	}
	require.NoError(t, env.csStream.Send(block2))

	txStatus, err = env.csStream.Recv()
	require.NoError(t, err)
	expectedTxStatus = &protocoordinatorservice.TxValidationStatusBatch{
		TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
			{
				TxId:   "tx2",
				Status: protoblocktx.Status_COMMITTED,
			},
			{
				TxId:   "mvcc conflict",
				Status: protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			},
			{
				TxId:   "tx1",
				Status: protoblocktx.Status_ABORTED_DUPLICATE_TXID,
			},
		},
	}
	require.ElementsMatch(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)
	require.Equal(t, uint64(3), env.coordinator.nextExpectedBlockNumberToBeReceived)

	cancel()
	env.grpcServer.Stop()

	ctx, cancel = context.WithCancel(context.Background())
	t.Cleanup(cancel)
	vcEnv := vcservice.NewValidatorAndCommitServiceTestEnv(ctx, t, env.dbEnv)
	env.config.ValidatorCommitterConfig.ServerConfig = []*connection.ServerConfig{vcEnv.Config.Server}
	env.coordinator = NewCoordinatorService(env.config)
	env.start(ctx, t)

	require.Eventually(t, func() bool {
		return uint64(2) == env.coordinator.nextExpectedBlockNumberToBeReceived
	}, 4*time.Second, 250*time.Millisecond)

	env.dbEnv.StatusExistsForNonDuplicateTxID(t, map[string]protoblocktx.Status{
		"tx2":           protoblocktx.Status_COMMITTED,
		"mvcc conflict": protoblocktx.Status_ABORTED_MVCC_CONFLICT,
		"tx1":           protoblocktx.Status_ABORTED_DUPLICATE_TXID,
	}, map[vcservice.TxID]*types.Height{
		"tx2":           types.NewHeight(2, 0),
		"mvcc conflict": types.NewHeight(2, 2),
		"tx1":           types.NewHeight(2, 5),
	})

	// Now, we are sending the full block 2.
	block2 = &protoblocktx.Block{
		Number: 2,
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
			{
				Id: "mvcc conflict",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      2,
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
			{
				Id: "duplicate namespace",
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
								Value: nsPolicy,
							},
						},
					},
					{
						NsId:      1,
						NsVersion: types.VersionNumber(0).Bytes(),
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
					[]byte("dummy"),
					[]byte("dummy"),
				},
			},
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
		TxsNum: []uint32{0, 1, 2, 4, 5},
	}

	require.NoError(t, env.csStream.Send(block2))

	var actualTxsStatus []*protocoordinatorservice.TxValidationStatus
	for len(actualTxsStatus) < 4 {
		statusBatch, err := env.csStream.Recv()
		require.NoError(t, err)
		actualTxsStatus = append(actualTxsStatus, statusBatch.TxsValidationStatus...)
	}
	expectedTxStatus = &protocoordinatorservice.TxValidationStatusBatch{
		TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
			{
				TxId:   "tx2",
				Status: protoblocktx.Status_COMMITTED,
			},
			{
				TxId:   "tx3",
				Status: protoblocktx.Status_COMMITTED,
			},
			{
				TxId:   "mvcc conflict",
				Status: protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			},
			{
				TxId:   "duplicate namespace",
				Status: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
			},
			{
				TxId:   "tx1",
				Status: protoblocktx.Status_ABORTED_DUPLICATE_TXID,
			},
		},
	}

	require.Len(t, actualTxsStatus, len(expectedTxStatus.TxsValidationStatus))
	require.ElementsMatch(t, expectedTxStatus.TxsValidationStatus, actualTxsStatus)
	require.Equal(t, uint64(3), env.coordinator.nextExpectedBlockNumberToBeReceived)
}

func TestGRPCConnectionFailure(t *testing.T) {
	c := NewCoordinatorService(fakeConfigForTest(t))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := c.Run(ctx)
	require.ErrorContains(t, err, "transport: Error while dialing: dial tcp: lookup random")
	require.ErrorContains(t, err, "no such host")
}

func TestConnectionReadyWithTimeout(t *testing.T) {
	c := NewCoordinatorService(fakeConfigForTest(t))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.Never(t, func() bool {
		c.WaitForReady(ctx)
		return true
	}, 5*time.Second, 1*time.Second)
}

func TestChunkSizeSentForDepGraph(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env := newCoordinatorTestEnv(ctx, t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: true})
	env.start(ctx, t)

	txPerBlock := 1990
	txs := make([]*protoblocktx.Tx, txPerBlock)
	txsNum := make([]uint32, txPerBlock)
	expectedTxsStatus := make([]*protocoordinatorservice.TxValidationStatus, txPerBlock)
	for i := 0; i < txPerBlock; i++ {
		txs[i] = &protoblocktx.Tx{
			Id: "tx" + strconv.Itoa(i),
			Namespaces: []*protoblocktx.TxNamespace{
				{
					NsId:      1,
					NsVersion: types.VersionNumber(0).Bytes(),
					BlindWrites: []*protoblocktx.Write{
						{
							Key: []byte("key" + strconv.Itoa(i)),
						},
					},
				},
			},
			Signatures: [][]byte{
				[]byte("dummy"),
			},
		}
		txsNum[i] = uint32(i) // nolint:gosec
		expectedTxsStatus[i] = &protocoordinatorservice.TxValidationStatus{
			TxId:   "tx" + strconv.Itoa(i),
			Status: protoblocktx.Status_COMMITTED,
		}
	}

	err := env.csStream.Send(&protoblocktx.Block{
		Number: 0,
		Txs:    txs,
		TxsNum: txsNum,
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal) == float64(txPerBlock)
	}, 4*time.Second, 100*time.Millisecond)

	var actualTxsStatus []*protocoordinatorservice.TxValidationStatus
	for len(actualTxsStatus) < txPerBlock {
		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		actualTxsStatus = append(actualTxsStatus, txStatus.TxsValidationStatus...)
	}

	require.ElementsMatch(t, expectedTxsStatus, actualTxsStatus)
	statusSentTotal := test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal)
	require.Equal(t, float64(txPerBlock), statusSentTotal)
}

func fakeConfigForTest(_ *testing.T) *CoordinatorConfig {
	return &CoordinatorConfig{
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
	}
}
