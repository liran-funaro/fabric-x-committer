package coordinatorservice

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

func TestCoordinatorServiceBadTxFormat(t *testing.T) {
	env := newCoordinatorTestEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	env.start(ctx, t)

	nsPolicy, err := proto.Marshal(&protoblocktx.NamespacePolicy{
		Scheme:    "ECDSA",
		PublicKey: []byte("publicKey"),
	})
	require.NoError(t, err)

	tests := []struct {
		name           string
		tx             *protoblocktx.Tx
		expectedStatus protoblocktx.Status
	}{
		{
			name:           "missing tx id",
			tx:             &protoblocktx.Tx{},
			expectedStatus: protoblocktx.Status_ABORTED_MISSING_TXID,
		},
		{
			name: "invalid signature",
			tx: &protoblocktx.Tx{
				Id:         "tx1",
				Signatures: [][]byte{[]byte("dummy")},
			},
			expectedStatus: protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
		},
		{
			name: "missing namespace version",
			tx: &protoblocktx.Tx{
				Id: "tx1",
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
			name: "no writes",
			tx: &protoblocktx.Tx{
				Id: "tx1",
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
			name: "namespace id is invalid in metaNs tx",
			tx: &protoblocktx.Tx{
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
			name: "namespace policy is invalid in metaNs tx",
			tx: &protoblocktx.Tx{
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
			name: "duplicate namespace",
			tx: &protoblocktx.Tx{
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
			name: "blind writes not allowed in metaNs tx",
			tx: &protoblocktx.Tx{
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

	blockNumber := uint64(0)
	receivedTx := 0
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err = env.csStream.Send(&protoblocktx.Block{
				Number: blockNumber,
				Txs: []*protoblocktx.Tx{
					tt.tx,
				},
			})
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				totalReceivedTx := test.GetMetricValue(t, env.coordinator.metrics.transactionReceivedTotal)
				if receivedTx+1 == int(totalReceivedTx) {
					receivedTx = int(totalReceivedTx)
					return true
				}
				return false
			}, 1*time.Second, 100*time.Millisecond)

			txStatus, err := env.csStream.Recv()
			require.NoError(t, err)
			expectedTxStatus := &protocoordinatorservice.TxValidationStatusBatch{
				TxsValidationStatus: []*protocoordinatorservice.TxValidationStatus{
					{
						TxId:   tt.tx.Id,
						Status: tt.expectedStatus,
					},
				},
			}
			require.Equal(t, expectedTxStatus.TxsValidationStatus, txStatus.TxsValidationStatus)
			require.Equal(
				t,
				float64(0),
				test.GetMetricValue(t, env.coordinator.metrics.transactionCommittedStatusSentTotal),
			)
			blockNumber++
		})
	}
}
