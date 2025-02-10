package coordinatorservice

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/dependencygraph"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

type (
	coordinatorTestEnv struct {
		coordinator            *CoordinatorService
		config                 *CoordinatorConfig
		client                 protocoordinatorservice.CoordinatorClient
		csStream               protocoordinatorservice.Coordinator_BlockProcessingClient
		streamCancel           context.CancelFunc
		dbEnv                  *vcservice.DatabaseTestEnv
		sigVerifiers           []*mock.SigVerifier
		sigVerifierGrpcServers *test.GrpcServers
	}

	testConfig struct {
		numSigService int
		numVcService  int
		mockVcService bool
	}
)

func newCoordinatorTestEnv(t *testing.T, tConfig *testConfig) *coordinatorTestEnv {
	svs, svServers := mock.StartMockSVService(t, tConfig.numSigService)

	vcServerConfigs := make([]*connection.ServerConfig, 0, tConfig.numVcService)
	var vcsTestEnv *vcservice.ValidatorAndCommitterServiceTestEnv

	if !tConfig.mockVcService {
		vcsTestEnv = vcservice.NewValidatorAndCommitServiceTestEnv(t, tConfig.numVcService)
		for _, c := range vcsTestEnv.Configs {
			vcServerConfigs = append(vcServerConfigs, c.Server)
		}
	} else {
		_, vcServers := mock.StartMockVCService(t, tConfig.numVcService)
		vcServerConfigs = vcServers.Configs
	}

	c := &CoordinatorConfig{
		SignVerifierConfig: &SignVerifierConfig{
			ServerConfig: svServers.Configs,
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
		coordinator:            NewCoordinatorService(c),
		config:                 c,
		dbEnv:                  vcsTestEnv.GetDBEnv(t),
		sigVerifiers:           svs,
		sigVerifierGrpcServers: svServers,
	}
}

func (e *coordinatorTestEnv) start(ctx context.Context, t *testing.T) {
	cs := e.coordinator
	sc := &connection.ServerConfig{
		Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: 0,
		},
	}
	test.RunServiceAndGrpcForTest(ctx, t, cs, sc, func(server *grpc.Server) {
		protocoordinatorservice.RegisterCoordinatorServer(server, cs)
	})

	conn, err := connection.Connect(connection.NewDialConfig(&sc.Endpoint))
	require.NoError(t, err)

	client := protocoordinatorservice.NewCoordinatorClient(conn)
	e.client = client

	sCtx, sCancel := context.WithTimeout(ctx, 2*time.Minute)
	t.Cleanup(sCancel)
	csStream, err := client.BlockProcessing(sCtx)
	require.NoError(t, err)

	e.csStream = csStream
	e.streamCancel = sCancel
}

func (e *coordinatorTestEnv) ensureStreamActive(t *testing.T) {
	require.Eventually(t, func() bool {
		if !e.coordinator.streamActive.TryLock() {
			return true
		}
		defer e.coordinator.streamActive.Unlock()
		return false
	}, 4*time.Second, 250*time.Millisecond)
}

func (e *coordinatorTestEnv) createNamespace(t *testing.T, blkNum int, nsID string) {
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
						NsId:      types.MetaNamespaceID,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   []byte(nsID),
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
	expectedTxStatus := map[string]*protoblocktx.StatusWithHeight{
		txID: {Code: protoblocktx.Status_COMMITTED},
	}
	require.Equal(t, expectedTxStatus, txStatus.Status)
}

func TestCoordinatorOneActiveStreamOnly(t *testing.T) {
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	env.ensureStreamActive(t)

	stream, err := env.client.BlockProcessing(ctx)
	require.NoError(t, err)
	_, err = stream.Recv()
	require.ErrorContains(t, err, ErrExistingStreamOrConflictingOp.Error())
}

func TestGetNextBlockNumWithActiveStream(t *testing.T) {
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	env.ensureStreamActive(t)

	blkInfo, err := env.client.GetNextExpectedBlockNumber(ctx, nil)
	require.ErrorContains(t, err, ErrActiveStreamBlockNumber.Error())
	require.Nil(t, blkInfo)
}

func TestCoordinatorServiceValidTx(t *testing.T) {
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: false})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	env.createNamespace(t, 0, "1")

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
						NsId:      "1",
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("key"),
							},
						},
					},
					{
						NsId:      types.MetaNamespaceID,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   []byte("2"),
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
	expectedTxStatus := map[string]*protoblocktx.StatusWithHeight{
		"tx1": {Code: protoblocktx.Status_COMMITTED, BlockNumber: 1},
	}

	require.Equal(t, expectedTxStatus, txStatus.Status)
	test.EnsurePersistedTxStatus(ctx, t, env.client, []string{"tx1"}, expectedTxStatus)

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

func TestCoordinatorServiceDependentOrderedTxs(t *testing.T) {
	// TODO: Use real signature verifier instead of mocks.
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: false})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	utNsID := "1"
	utNsVersion := types.VersionNumber(0).Bytes()
	mainKey := []byte("main-key")
	subKey := []byte("sub-key")
	p := &protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("public-key"),
	}
	pBytes, err := proto.Marshal(p)
	require.NoError(t, err)

	// We send a block with a series of TXs with apparent conflicts, but all should be committed successfully if
	// executed serially.
	b1 := &protoblocktx.Block{
		Number: 0,
		Txs: []*protoblocktx.Tx{
			{
				Id: "create namespace 1",
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:      types.MetaNamespaceID,
					NsVersion: types.VersionNumber(0).Bytes(),
					ReadWrites: []*protoblocktx.ReadWrite{
						{
							Key:   []byte(utNsID),
							Value: pBytes,
						},
					},
				}},
			},
			{
				Id: "create main key (read-write version 0)",
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:      utNsID,
					NsVersion: utNsVersion,
					ReadWrites: []*protoblocktx.ReadWrite{{
						Key:   mainKey,
						Value: []byte("value of version 0"),
					}},
				}},
			},
			{
				Id: "update main key (read-write version 1)",
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:      utNsID,
					NsVersion: utNsVersion,
					ReadWrites: []*protoblocktx.ReadWrite{{
						Key:     mainKey,
						Value:   []byte("value of version 1"),
						Version: types.VersionNumber(0).Bytes(),
					}},
				}},
			},
			{
				Id: "update main key (blind-write version 2)",
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:      utNsID,
					NsVersion: utNsVersion,
					BlindWrites: []*protoblocktx.Write{{
						Key:   mainKey,
						Value: []byte("Value of version 2"),
					}},
				}},
			},
			{
				Id: "read main key, create sub key (read version 2, read-write version 0)",
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:      utNsID,
					NsVersion: utNsVersion,
					ReadsOnly: []*protoblocktx.Read{{
						Key:     mainKey,
						Version: types.VersionNumber(2).Bytes(),
					}},
					ReadWrites: []*protoblocktx.ReadWrite{{
						Key:   subKey,
						Value: []byte("Sub value of version 0"),
					}},
				}},
			},
			{
				Id: "update main key (read-write version 3)",
				Namespaces: []*protoblocktx.TxNamespace{{
					NsId:      utNsID,
					NsVersion: utNsVersion,
					ReadWrites: []*protoblocktx.ReadWrite{{
						Key:     mainKey,
						Version: types.VersionNumber(2).Bytes(),
						Value:   []byte("Value of version 3"),
					}},
				}},
			},
		},
	}
	for i, tx := range b1.Txs {
		tx.Signatures = [][]byte{
			[]byte("dummy"),
		}
		b1.TxsNum = append(b1.TxsNum, uint32(i)) // nolint:gosec // integer overflow conversion int -> uint32
	}

	expectedReceived := test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal) + float64(len(b1.Txs))

	require.NoError(t, env.csStream.Send(b1))
	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal) >= expectedReceived
	}, 5*time.Second, 500*time.Millisecond)

	status := make(map[string]*protoblocktx.StatusWithHeight)
	require.Eventually(t, func() bool {
		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		for id, s := range txStatus.Status {
			status[id] = s
		}
		return len(status) == len(b1.Txs)
	}, 20*time.Second, 500*time.Millisecond)
	for txID, txStatus := range status {
		require.Equal(t, protoblocktx.Status_COMMITTED, txStatus.Code, txID)
	}
	require.Equal(
		t,
		expectedReceived,
		test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
	)

	res := env.dbEnv.FetchKeys(t, utNsID, [][]byte{mainKey, subKey})
	mainValue, ok := res[string(mainKey)]
	require.True(t, ok)
	require.Equal(t, types.VersionNumber(3).Bytes(), mainValue.Version)

	subValue, ok := res[string(subKey)]
	require.True(t, ok)
	require.Equal(t, types.VersionNumber(0).Bytes(), subValue.Version)
}

func TestQueueSize(t *testing.T) { // nolint:gocognit
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	go env.coordinator.monitorQueues(ctx)

	q := env.coordinator.queues
	m := env.coordinator.metrics
	q.depGraphToSigVerifierFreeTxs <- dependencygraph.TxNodeBatch{}
	q.sigVerifierToVCServiceValidatedTxs <- dependencygraph.TxNodeBatch{}
	q.vcServiceToDepGraphValidatedTxs <- dependencygraph.TxNodeBatch{}
	q.vcServiceToCoordinatorTxStatus <- &protoblocktx.TransactionsStatus{}

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, m.sigverifierInputTxBatchQueueSize) == 1 &&
			test.GetMetricValue(t, m.sigverifierOutputValidatedTxBatchQueueSize) == 1 &&
			test.GetMetricValue(t, m.vcserviceOutputValidatedTxBatchQueueSize) == 1 &&
			test.GetMetricValue(t, m.vcserviceOutputTxStatusBatchQueueSize) == 1
	}, 3*time.Second, 500*time.Millisecond)

	<-q.depGraphToSigVerifierFreeTxs
	<-q.sigVerifierToVCServiceValidatedTxs
	<-q.vcServiceToDepGraphValidatedTxs
	<-q.vcServiceToCoordinatorTxStatus

	require.Eventually(t, func() bool {
		return test.GetMetricValue(t, m.sigverifierInputTxBatchQueueSize) == 0 &&
			test.GetMetricValue(t, m.sigverifierOutputValidatedTxBatchQueueSize) == 0 &&
			test.GetMetricValue(t, m.vcserviceOutputValidatedTxBatchQueueSize) == 0 &&
			test.GetMetricValue(t, m.vcserviceOutputTxStatusBatchQueueSize) == 0
	}, 3*time.Second, 500*time.Millisecond)
}

func TestCoordinatorRecovery(t *testing.T) {
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: false})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	require.Equal(t, uint64(0), env.coordinator.nextExpectedBlockNumberToBeReceived.Load())
	env.createNamespace(t, 0, "1")
	require.Equal(t, uint64(1), env.coordinator.nextExpectedBlockNumberToBeReceived.Load())

	err := env.csStream.Send(&protoblocktx.Block{
		Number: 1,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx1",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      "1",
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
	expectedTxStatus := map[string]*protoblocktx.StatusWithHeight{
		"tx1": {Code: protoblocktx.Status_COMMITTED, BlockNumber: 1},
	}
	require.Equal(t, expectedTxStatus, txStatus.Status)
	test.EnsurePersistedTxStatus(ctx, t, env.client, []string{"tx1"}, expectedTxStatus)

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
						NsId:      "1",
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
						NsId:      "2",
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
						NsId:      "1",
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
	expectedTxStatus = map[string]*protoblocktx.StatusWithHeight{
		"tx2":           types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 2, 0),
		"mvcc conflict": types.CreateStatusWithHeight(protoblocktx.Status_ABORTED_MVCC_CONFLICT, 2, 2),
		"tx1":           types.CreateStatusWithHeight(protoblocktx.Status_ABORTED_DUPLICATE_TXID, 2, 5),
	}
	require.Equal(t, expectedTxStatus, txStatus.Status)
	require.Equal(t, uint64(3), env.coordinator.nextExpectedBlockNumberToBeReceived.Load())

	expectedTxStatus["tx1"] = types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 1, 0)
	test.EnsurePersistedTxStatus(ctx, t, env.client, []string{"tx2", "mvcc conflict", "tx1"}, expectedTxStatus)

	cancel()

	vcEnv := vcservice.NewValidatorAndCommitServiceTestEnv(t, 1, env.dbEnv)
	env.config.ValidatorCommitterConfig.ServerConfig = []*connection.ServerConfig{vcEnv.Configs[0].Server}
	env.coordinator = NewCoordinatorService(env.config)
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	require.Eventually(t, func() bool {
		return uint64(2) == env.coordinator.nextExpectedBlockNumberToBeReceived.Load()
	}, 4*time.Second, 250*time.Millisecond)

	env.dbEnv.StatusExistsForNonDuplicateTxID(t, expectedTxStatus)

	// Now, we are sending the full block 2.
	block2 = &protoblocktx.Block{
		Number: 2,
		Txs: []*protoblocktx.Tx{
			{
				Id: "tx2",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      "1",
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
						NsId:      "1",
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
						NsId:      "2",
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
						NsId:      "1",
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key: []byte("key"),
							},
						},
					},
					{
						NsId:      types.MetaNamespaceID,
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadWrites: []*protoblocktx.ReadWrite{
							{
								Key:   []byte("2"),
								Value: nsPolicy,
							},
						},
					},
					{
						NsId:      "1",
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
						NsId:      "1",
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

	actualTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for len(actualTxsStatus) < 5 {
		statusBatch, err := env.csStream.Recv()
		require.NoError(t, err)
		for id, s := range statusBatch.Status {
			actualTxsStatus[id] = s
		}
	}
	expectedTxStatus = map[string]*protoblocktx.StatusWithHeight{
		"tx2":                 types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 2, 0),
		"tx3":                 types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 2, 1),
		"mvcc conflict":       types.CreateStatusWithHeight(protoblocktx.Status_ABORTED_MVCC_CONFLICT, 2, 2),
		"duplicate namespace": types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 2, 4),
		"tx1":                 types.CreateStatusWithHeight(protoblocktx.Status_ABORTED_DUPLICATE_TXID, 2, 5),
	}

	require.Len(t, actualTxsStatus, len(expectedTxStatus))
	require.Equal(t, expectedTxStatus, actualTxsStatus)
	require.Equal(t, uint64(3), env.coordinator.nextExpectedBlockNumberToBeReceived.Load())

	expectedTxStatus["tx1"] = types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 1, 0)
	test.EnsurePersistedTxStatus(ctx, t,
		env.client, []string{"tx2", "tx3", "mvcc conflict", "duplicate namespace", "tx1"}, expectedTxStatus)
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
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: true})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	txPerBlock := 1990
	txs := make([]*protoblocktx.Tx, txPerBlock)
	txsNum := make([]uint32, txPerBlock)
	expectedTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for i := 0; i < txPerBlock; i++ {
		txs[i] = &protoblocktx.Tx{
			Id: "tx" + strconv.Itoa(i),
			Namespaces: []*protoblocktx.TxNamespace{
				{
					NsId:      "1",
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
		expectedTxsStatus["tx"+strconv.Itoa(i)] = types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 0, i)
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

	actualTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for len(actualTxsStatus) < txPerBlock {
		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		for id, s := range txStatus.Status {
			actualTxsStatus[id] = s
		}
	}

	require.Equal(t, expectedTxsStatus, actualTxsStatus)
	statusSentTotal := test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal)
	require.Equal(t, float64(txPerBlock), statusSentTotal)
}

func TestWaitingTxsCount(t *testing.T) {
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 1, numVcService: 1, mockVcService: true})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	txPerBlock := 10
	txs := make([]*protoblocktx.Tx, txPerBlock)
	txsNum := make([]uint32, txPerBlock)
	expectedTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for i := 0; i < txPerBlock; i++ {
		txs[i] = &protoblocktx.Tx{
			Id: "tx" + strconv.Itoa(i),
			Namespaces: []*protoblocktx.TxNamespace{
				{
					NsId:      "1",
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
		expectedTxsStatus["tx"+strconv.Itoa(i)] = types.CreateStatusWithHeight(protoblocktx.Status_COMMITTED, 0, i)
	}

	success := make(chan bool, 1)
	go func() {
		require.Eventually(t, func() bool {
			if env.coordinator.numWaitingTxsForStatus.Load() == int32(2) {
				success <- true
				return true
			}
			return false
		}, 3*time.Second, 250*time.Millisecond)
		close(success)
	}()

	env.sigVerifiers[0].MockFaultyNodeDropSize = 2
	err := env.csStream.Send(&protoblocktx.Block{
		Number: 0,
		Txs:    txs,
		TxsNum: txsNum,
	})
	require.NoError(t, err)
	require.True(t, <-success)

	count, err := env.client.NumberOfWaitingTransactionsForStatus(context.Background(), nil)
	require.Contains(t, err.Error(), "stream is still active")
	require.Nil(t, count)

	env.sigVerifierGrpcServers.Servers[0].Stop()
	require.Eventually(t, func() bool {
		return test.CheckServerStopped(t, env.sigVerifierGrpcServers.Configs[0].Endpoint.Address())
	}, 4*time.Second, 500*time.Millisecond)

	env.sigVerifiers[0].MockFaultyNodeDropSize = 0
	env.sigVerifierGrpcServers = mock.StartMockSVServiceFromListWithConfig(
		t,
		env.sigVerifiers,
		env.sigVerifierGrpcServers.Configs,
	)

	actualTxsStatus := make(map[string]*protoblocktx.StatusWithHeight)
	for len(actualTxsStatus) < txPerBlock {
		txStatus, err := env.csStream.Recv()
		require.NoError(t, err)
		for id, s := range txStatus.Status {
			actualTxsStatus[id] = s
		}
	}

	require.Equal(t, expectedTxsStatus, actualTxsStatus)
	statusSentTotal := test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal)
	require.Equal(t, float64(txPerBlock), statusSentTotal)

	env.streamCancel()
	require.Eventually(t, func() bool {
		if !env.coordinator.streamActive.TryLock() {
			return false
		}
		defer env.coordinator.streamActive.Unlock()
		return true
	}, 2*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		wTxs, err := env.client.NumberOfWaitingTransactionsForStatus(context.Background(), nil)
		if err != nil {
			return false
		}
		return wTxs.GetCount() == 0
	}, 2*time.Second, 100*time.Millisecond)
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
