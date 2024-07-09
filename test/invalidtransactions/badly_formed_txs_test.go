package invalidtransactions_test

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/test/cluster"
)

func TestBadlyFormedTxs(t *testing.T) {
	RegisterTestingT(t)
	c := cluster.NewCluster(
		t,
		&cluster.Config{
			NumSigVerifiers: 2,
			NumVCService:    2,
		},
	)
	defer c.Stop(t)

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
			name: "missing entries",
			txs: []*protoblocktx.Tx{
				{
					Id: "",
				},
				{
					Id: "missing signature",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: 1,
						},
					},
				},
				{
					Id: "missing namespace",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: 1,
						},
					},
					Signatures: [][]byte{[]byte("signature")},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"":                  protoblocktx.Status_ABORTED_MISSING_TXID,
				"missing signature": protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
				"missing namespace": protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION,
			},
		},
		{
			name: "invalid namespace tx",
			txs: []*protoblocktx.Tx{
				{
					Id: "blind writes not allowed in ns lifecycle",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      uint32(types.MetaNamespaceID),
							NsVersion: types.VersionNumber(0).Bytes(),
							BlindWrites: []*protoblocktx.Write{
								{
									Key: []byte("key1"),
								},
							},
						},
					},
					Signatures: [][]byte{[]byte("signature")},
				},
				{
					Id: "invalid namespace id in ns lifecycle",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      uint32(types.MetaNamespaceID),
							NsVersion: types.VersionNumber(0).Bytes(),
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key: []byte("key1"),
								},
							},
						},
					},
					Signatures: [][]byte{[]byte("signature")},
				},
				{
					Id: "invalid policy in ns lifecycle",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      uint32(types.MetaNamespaceID),
							NsVersion: types.VersionNumber(0).Bytes(),
							ReadWrites: []*protoblocktx.ReadWrite{
								{
									Key:   types.NamespaceID(1).Bytes(),
									Value: []byte("policy"),
								},
							},
						},
					},
					Signatures: [][]byte{[]byte("signature")},
				},
				{
					Id: "invalid signature",
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
					Signatures: [][]byte{[]byte("signature")},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"blind writes not allowed in ns lifecycle": protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
				"invalid namespace id in ns lifecycle":     protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
				"invalid policy in ns lifecycle":           protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID,
				"invalid signature":                        protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
			},
		},
		{
			name: "duplicate namespace",
			txs: []*protoblocktx.Tx{
				{
					Id: "duplicate namespace",
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
					Signatures: [][]byte{[]byte("signature"), []byte("signature")},
				},
			},
			expectedTxStatus: map[string]protoblocktx.Status{
				"duplicate namespace": protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.SendTransactions(t, tt.txs)
			c.ValidateStatus(t, tt.expectedTxStatus)
		})
	}
}
