package namespacelifecycle_test

import (
	"testing"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/test/cluster"
)

func TestCreateUpdateNamespace(t *testing.T) {
	gomega.RegisterTestingT(t)
	c := cluster.NewCluster(
		t,
		&cluster.Config{
			NumSigVerifiers: 2,
			NumVCService:    2,
		},
	)
	defer c.Stop()

	c.CreateCryptoForNs(t, types.NamespaceID(1), &signature.Profile{Scheme: signature.Ecdsa})
	ns1Policy := &protoblocktx.NamespacePolicy{
		Scheme:    signature.Ecdsa,
		PublicKey: c.GetPublicKey(t, types.NamespaceID(1)),
	}
	policyBytes, err := proto.Marshal(ns1Policy)
	require.NoError(t, err)

	tests := []struct {
		name             string
		txs              []*protoblocktx.Tx
		expectedTxStatus map[string]protoblocktx.Status
	}{
		{
			name: "create namespace ns1",
			txs: []*protoblocktx.Tx{
				{
					Id: "create ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      uint32(types.MetaNamespaceID),
							NsVersion: types.VersionNumber(0).Bytes(),
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:   types.NamespaceID(1).Bytes(),
									Value: policyBytes,
								},
							},
						},
					},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"create ns 1": protoblocktx.Status_COMMITTED,
			},
		},
		{
			name: "write to namespace ns1",
			txs: []*protoblocktx.Tx{
				{
					Id: "write to ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: types.VersionNumber(0).Bytes(),
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte("key1"),
									Value: []byte("value1"),
								},
							},
						},
					},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"write to ns 1": protoblocktx.Status_COMMITTED,
			},
		},
		{
			name: "update namespace ns1",
			txs: []*protoblocktx.Tx{
				{
					Id: "write to ns 1 before updating ns1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: types.VersionNumber(0).Bytes(),
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte("key2"),
									Value: []byte("value2"),
								},
							},
						},
					},
				},
				{
					Id: "update ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      uint32(types.MetaNamespaceID),
							NsVersion: types.VersionNumber(0).Bytes(),
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:     types.NamespaceID(1).Bytes(),
									Version: types.VersionNumber(0).Bytes(),
									Value:   policyBytes,
								},
							},
						},
					},
				},
				{
					Id: "write to stale ns 1",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: types.VersionNumber(0).Bytes(),
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte("key3"),
									Value: []byte("value3"),
								},
							},
						},
					},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"write to ns 1 before updating ns1": protoblocktx.Status_COMMITTED,
				"update ns 1":                       protoblocktx.Status_COMMITTED,
				"write to stale ns 1":               protoblocktx.Status_ABORTED_MVCC_CONFLICT,
			},
		},
		{
			name: "write again to namespace ns1",
			txs: []*protoblocktx.Tx{
				{
					Id: "write to ns1 again",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: types.VersionNumber(1).Bytes(),
							BlindWrites: []*protoblocktx.Write{
								{
									Key:   []byte("key4"),
									Value: []byte("value4"),
								},
							},
						},
					},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"write to ns1 again": protoblocktx.Status_COMMITTED,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, tx := range tt.txs {
				c.AddSignatures(t, tx)
			}
			c.SendTransactions(t, tt.txs)

			c.ValidateStatus(t, tt.expectedTxStatus)
		})
	}
}
