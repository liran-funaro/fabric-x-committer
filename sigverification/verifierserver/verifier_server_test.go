package verifierserver

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
	"google.golang.org/protobuf/proto"
)

const testTimeout = 3 * time.Second

var parallelExecutionConfig = &parallelexecutor.Config{
	BatchSizeCutoff:   3,
	BatchTimeCutoff:   1 * time.Hour,
	Parallelism:       3,
	ChannelBufferSize: 1,
}

var parallelExecutionConfigQuick = &parallelexecutor.Config{
	BatchSizeCutoff:   1,
	BatchTimeCutoff:   1 * time.Hour,
	Parallelism:       3,
	ChannelBufferSize: 1,
}

func TestNoVerificationKeySet(t *testing.T) {
	t.Skip("Temporarily skipping. Related to commented-out error in StartStream.")
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(t, New(parallelExecutionConfig, m))

	stream, err := c.Client.StartStream(context.Background())
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = stream.Send(&sigverification.RequestBatch{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	_, err = stream.Recv()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}

func TestNoInput(t *testing.T) {
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(t, New(parallelExecutionConfig, m))

	_, verificationKey := sigverification_test.GetSignatureFactory(signature.Ecdsa).NewKeys()

	_, err := c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
		Policies: []*sigverification.PolicyItem{
			policy.MakePolicy(t, "1", &protoblocktx.NamespacePolicy{
				PublicKey: verificationKey,
				Scheme:    signature.Ecdsa,
			}),
		},
	})
	require.NoError(t, err)

	stream, _ := c.Client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	output := sigverification_test.OutputChannel(stream)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Eventually(output).WithTimeout(testTimeout).ShouldNot(gomega.Receive())
}

func TestMinimalInput(t *testing.T) {
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(t, New(parallelExecutionConfig, m))
	factory := sigverification_test.GetSignatureFactory(signature.Ecdsa)
	signingKey, verificationKey := factory.NewKeys()
	txSigner, _ := factory.NewSigner(signingKey)

	_, err := c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
		Policies: []*sigverification.PolicyItem{
			policy.MakePolicy(t, "1", &protoblocktx.NamespacePolicy{
				PublicKey: verificationKey,
				Scheme:    signature.Ecdsa,
			}),
		},
	})
	require.NoError(t, err)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	stream, _ := c.Client.StartStream(context.Background())

	tx1 := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      "1",
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0001"),
			}},
		}},
	}
	s, _ := txSigner.SignNs(tx1, 0)
	tx1.Signatures = append(tx1.Signatures, s)

	tx2 := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      "1",
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0010"),
			}},
		}},
	}

	s, _ = txSigner.SignNs(tx2, 0)
	tx2.Signatures = append(tx2.Signatures, s)

	tx3 := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      "1",
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0011"),
			}},
		}},
	}
	s, _ = txSigner.SignNs(tx3, 0)
	tx3.Signatures = append(tx3.Signatures, s)

	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: tx1},
		{BlockNum: 1, TxNum: 2, Tx: tx2},
		{BlockNum: 1, TxNum: 3, Tx: tx3},
	}})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	output := sigverification_test.OutputChannel(stream)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Eventually(output).WithTimeout(1 * time.Second).Should(gomega.Receive(gomega.HaveLen(3)))
}

func TestBadTxFormat(t *testing.T) {
	test.FailHandler(t)
	m, ok := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	require.True(t, ok)
	c := sigverification_test.NewTestState(t, New(parallelExecutionConfigQuick, m))

	_, verificationKey := sigverification_test.GetSignatureFactory(signature.Ecdsa).NewKeys()
	_, err := c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
		Policies: []*sigverification.PolicyItem{
			policy.MakePolicy(t, "1", &protoblocktx.NamespacePolicy{
				PublicKey: verificationKey,
				Scheme:    signature.Ecdsa,
			}),
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	t.Cleanup(cancel)
	stream, _ := c.Client.StartStream(ctx)

	nsPolicy, err := proto.Marshal(&protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	})
	require.NoError(t, err)

	blockNumber := uint64(1)
	for _, tt := range []struct {
		tx             *protoblocktx.Tx
		expectedStatus protoblocktx.Status
	}{
		{
			tx:             &protoblocktx.Tx{Namespaces: []*protoblocktx.TxNamespace{}},
			expectedStatus: protoblocktx.Status_ABORTED_MISSING_TXID,
		},
		{
			tx: &protoblocktx.Tx{
				Id:         "invalid signature",
				Signatures: [][]byte{[]byte("dummy")},
			},
			expectedStatus: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
		},
		{
			tx: &protoblocktx.Tx{
				Id: "missing namespace version",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId: "1",
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
			expectedStatus: protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION,
		},
		{
			tx: &protoblocktx.Tx{
				Id: "no writes",
				Namespaces: []*protoblocktx.TxNamespace{
					{
						NsId:      "1",
						NsVersion: types.VersionNumber(0).Bytes(),
						ReadsOnly: []*protoblocktx.Read{
							{
								Key: []byte("k1"),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
				},
			},
			expectedStatus: protoblocktx.Status_ABORTED_NO_WRITES,
		},
		{
			tx: &protoblocktx.Tx{
				Id: "namespace id is invalid in metaNs tx",
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
								// empty namespaceIDs are not allowed
								Key: []byte(""),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
					[]byte("dummy"),
				},
			},
			expectedStatus: protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
		},
		{
			tx: &protoblocktx.Tx{
				Id: "namespace policy is invalid in metaNs tx",
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
								Value: []byte("value"),
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
					[]byte("dummy"),
				},
			},
			expectedStatus: protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID,
		},
		{
			tx: &protoblocktx.Tx{
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
			expectedStatus: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
		},
		{
			tx: &protoblocktx.Tx{
				Id: "blind writes not allowed in metaNs tx",
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
						BlindWrites: []*protoblocktx.Write{
							{
								Key:   []byte("2"),
								Value: nsPolicy,
							},
						},
					},
				},
				Signatures: [][]byte{
					[]byte("dummy"),
					[]byte("dummy"),
				},
			},
			expectedStatus: protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
		},
	} {
		t.Run(tt.tx.Id, func(t *testing.T) {
			requireTestCase(t, stream, &testCase{
				blkNum:         blockNumber,
				txNum:          0,
				tx:             tt.tx,
				expectedStatus: tt.expectedStatus,
			})
			blockNumber++
		})
	}
}

func TestUpdatePolicies(t *testing.T) {
	t.Parallel()
	test.FailHandler(t)
	m, ok := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	require.True(t, ok)
	c := sigverification_test.NewTestState(t, New(parallelExecutionConfigQuick, m))
	stream, err := c.Client.StartStream(context.Background())
	require.NoError(t, err)

	ns1 := "ns1"
	ns2 := "ns2"
	tx := makeTX("update", ns1, ns2)

	t.Run("no partial valid update", func(t *testing.T) {
		ns1Policy, ns1Signer := makePolicyItem(t, ns1)
		ns2Policy, ns2Signer := makePolicyItem(t, ns2)
		_, err := c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
			Policies: []*sigverification.PolicyItem{ns1Policy, ns2Policy},
		})
		require.NoError(t, err)

		// We attempt a bad policies update.
		// We expect no update since one of the given policies are invalid.
		p3, _ := makePolicyItem(t, ns1)
		_, err = c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
			Policies: []*sigverification.PolicyItem{
				p3,
				policy.MakePolicy(t, ns2, &protoblocktx.NamespacePolicy{
					PublicKey: []byte("bad-key"),
					Scheme:    signature.Ecdsa,
				}),
			},
		})
		require.Error(t, err)

		sign(t, tx, ns1Signer, ns2Signer)
		requireTestCase(t, stream, &testCase{
			blkNum:         1,
			txNum:          1,
			tx:             tx,
			expectedStatus: protoblocktx.Status_COMMITTED,
		})
	})

	t.Run("partial update", func(t *testing.T) {
		ns1Policy, ns1Signer := makePolicyItem(t, ns1)
		ns2Policy, _ := makePolicyItem(t, ns2)
		_, err := c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
			Policies: []*sigverification.PolicyItem{ns1Policy, ns2Policy},
		})
		require.NoError(t, err)

		ns2PolicyUpdate, ns2Signer := makePolicyItem(t, ns2)
		_, err = c.Client.UpdatePolicies(context.Background(), &sigverification.Policies{
			Policies: []*sigverification.PolicyItem{ns2PolicyUpdate},
		})
		require.NoError(t, err)

		sign(t, tx, ns1Signer, ns2Signer)
		requireTestCase(t, stream, &testCase{
			blkNum:         1,
			txNum:          1,
			tx:             tx,
			expectedStatus: protoblocktx.Status_COMMITTED,
		})
	})
}

func TestParallelUpdatePolicies(t *testing.T) {
	t.Parallel()
	test.FailHandler(t)
	m, ok := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	require.True(t, ok)
	service := New(parallelExecutionConfigQuick, m)
	c := sigverification_test.NewTestState(t, service)

	ns := make([]string, 101)
	for i := range ns {
		ns[i] = fmt.Sprintf("%d", i)
	}

	// Each policy update will update a unique namespace, and the common namespace.
	updateCount := len(ns) - 1
	endBarrier := sync.WaitGroup{}
	endBarrier.Add(updateCount)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	uniqueNsSigners := make([]sigverification_test.NsSigner, updateCount)
	commonNsSigners := make([]sigverification_test.NsSigner, updateCount)
	for i := range updateCount {
		uniqueNsPolicy, uniqueNsSigner := makePolicyItem(t, ns[i])
		uniqueNsSigners[i] = uniqueNsSigner
		commonNsPolicy, commonNsSigner := makePolicyItem(t, ns[len(ns)-1])
		commonNsSigners[i] = commonNsSigner
		p := &sigverification.Policies{
			Policies: []*sigverification.PolicyItem{uniqueNsPolicy, commonNsPolicy},
		}
		go func() {
			defer endBarrier.Done()
			_, err := c.Client.UpdatePolicies(ctx, p)
			assert.NoError(t, err)
		}()
	}
	endBarrier.Wait()

	stream, err := c.Client.StartStream(ctx)
	require.NoError(t, err)

	// The following TX updates all the namespaces.
	// We attempt this TX with each of the attempted policies for the common namespace.
	// One and only one should succeed.
	tx := makeTX("all", ns...)
	success := 0
	for i := range updateCount {
		sign(t, tx, append(uniqueNsSigners, commonNsSigners[i])...)
		require.NoError(t, stream.Send(&sigverification.RequestBatch{
			Requests: []*sigverification.Request{
				{
					BlockNum: 0,
					TxNum:    0,
					Tx:       tx,
				},
			},
		}))

		txStatus, err := stream.Recv()
		require.NoError(t, err)
		require.NotNil(t, txStatus)
		require.Len(t, txStatus.Responses, 1)
		if txStatus.Responses[0].Status == protoblocktx.Status_COMMITTED {
			success++
		}
	}
	require.Equal(t, 1, success)

	// The following TX updates all the namespaces but the common one.
	// It must succeed.
	tx = makeTX("all", ns[:updateCount]...)
	sign(t, tx, uniqueNsSigners...)
	requireTestCase(t, stream, &testCase{
		blkNum:         1,
		txNum:          1,
		tx:             tx,
		expectedStatus: protoblocktx.Status_COMMITTED,
	})
}

type testCase struct {
	blkNum         uint64
	txNum          uint64
	tx             *protoblocktx.Tx
	expectedStatus protoblocktx.Status
}

func sign(t *testing.T, tx *protoblocktx.Tx, signers ...sigverification_test.NsSigner) {
	tx.Signatures = make([][]byte, len(signers))
	for i, s := range signers {
		s, err := s.SignNs(tx, i)
		require.NoError(t, err)
		tx.Signatures[i] = s
	}
}

func makeTX(name string, namespaces ...string) *protoblocktx.Tx {
	tx := &protoblocktx.Tx{
		Id:         name,
		Namespaces: make([]*protoblocktx.TxNamespace, len(namespaces)),
	}
	for i, ns := range namespaces {
		tx.Namespaces[i] = &protoblocktx.TxNamespace{
			NsId:      ns,
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0001"),
			}},
		}
	}
	return tx
}

func makePolicyItem(t *testing.T, ns string) (*sigverification.PolicyItem, sigverification_test.NsSigner) {
	factory := sigverification_test.GetSignatureFactory(signature.Ecdsa)
	signingKey, verificationKey := factory.NewKeys()
	txSigner, err := factory.NewSigner(signingKey)
	require.NoError(t, err)
	p := policy.MakePolicy(t, ns, &protoblocktx.NamespacePolicy{
		PublicKey: verificationKey,
		Scheme:    signature.Ecdsa,
	})
	return p, txSigner
}

func requireTestCase(
	t *testing.T,
	stream sigverification.Verifier_StartStreamClient,
	tt *testCase,
) {
	err := stream.Send(&sigverification.RequestBatch{
		Requests: []*sigverification.Request{
			{
				BlockNum: tt.blkNum,
				TxNum:    tt.txNum,
				Tx:       tt.tx,
			},
		},
	})
	require.NoError(t, err)

	txStatus, err := stream.Recv()
	require.NoError(t, err)
	require.NotNil(t, txStatus)
	require.Len(t, txStatus.Responses, 1)
	resp := txStatus.Responses[0]
	require.NotNil(t, resp)
	require.Equal(t, tt.blkNum, resp.BlockNum)
	require.Equal(t, tt.txNum, resp.TxNum)
	require.Equal(t, tt.tx.Id, resp.TxId)
	require.Equal(t, tt.expectedStatus, resp.Status)
}
