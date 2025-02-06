package verifierserver_test

import (
	"context"
	"testing"
	"time"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/policy"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
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

func TestNoVerificationKeySet(t *testing.T) {
	t.Skip("Temporarily skipping. Related to commented-out error in StartStream.")
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(t, verifierserver.New(parallelExecutionConfig, m))

	stream, err := c.Client.StartStream(context.Background())
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = stream.Send(&sigverification.RequestBatch{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	_, err = stream.Recv()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	c.TearDown()
}

func TestNoInput(t *testing.T) {
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(t, verifierserver.New(parallelExecutionConfig, m))

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

	c.TearDown()
}

func TestMinimalInput(t *testing.T) {
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(t, verifierserver.New(parallelExecutionConfig, m))
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

	c.TearDown()
}

func TestBadTxFormat(t *testing.T) {
	test.FailHandler(t)
	m, ok := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	require.True(t, ok)
	c := sigverification_test.NewTestState(t, verifierserver.New(&parallelexecutor.Config{
		BatchSizeCutoff:   1,
		BatchTimeCutoff:   1 * time.Hour,
		Parallelism:       3,
		ChannelBufferSize: 1,
	}, m))

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
			txNum := uint64(0)
			err = stream.Send(&sigverification.RequestBatch{
				Requests: []*sigverification.Request{
					{
						BlockNum: blockNumber,
						TxNum:    txNum,
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
			require.Equal(t, blockNumber, resp.BlockNum)
			require.Equal(t, txNum, resp.TxNum)
			require.Equal(t, tt.tx.Id, resp.TxId)
			require.Equal(t, tt.expectedStatus, resp.Status)
			blockNumber++
		})
	}
}
