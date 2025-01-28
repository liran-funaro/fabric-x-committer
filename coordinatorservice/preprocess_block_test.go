package coordinatorservice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

func TestCoordinatorServiceBadTxFormat(t *testing.T) {
	env := newCoordinatorTestEnv(t, &testConfig{numSigService: 2, numVcService: 2, mockVcService: false})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	env.createNamespace(t, 0, 1)

	nsPolicy, err := proto.Marshal(&protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	})
	require.NoError(t, err)

	tests := []struct {
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
						NsId: 1,
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
						NsId:      1,
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
								Key: []byte("key"),
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
			expectedStatus: protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
		},
		{
			tx: &protoblocktx.Tx{
				Id: "blind writes not allowed in metaNs tx",
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
						BlindWrites: []*protoblocktx.Write{
							{
								Key:   types.NamespaceID(2).Bytes(),
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
	}

	blockNumber := uint64(1)
	receivedTxCount := test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal)
	commitStatusCount := test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal)
	for _, tt := range tests {
		t.Run(tt.tx.GetId(), func(t *testing.T) {
			err = env.csStream.Send(&protoblocktx.Block{
				Number: blockNumber,
				Txs: []*protoblocktx.Tx{
					tt.tx,
				},
				TxsNum: []uint32{0},
			})
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				totalReceivedTx := test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal)
				if receivedTxCount+1 == totalReceivedTx {
					receivedTxCount = totalReceivedTx
					return true
				}
				return false
			}, 2*time.Second, 100*time.Millisecond)

			txStatus, err := env.csStream.Recv()
			require.NoError(t, err)
			expectedTxStatus := map[string]*protoblocktx.StatusWithHeight{
				tt.tx.Id: types.CreateStatusWithHeight(tt.expectedStatus, blockNumber, 0),
			}
			require.Equal(t, expectedTxStatus, txStatus.Status)
			require.Equal(
				t,
				commitStatusCount,
				test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
			)
			blockNumber++
		})
	}
}
