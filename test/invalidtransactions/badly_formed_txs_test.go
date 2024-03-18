package invalidtransactions_test

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/test/cluster"
	"google.golang.org/protobuf/proto"
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
	defer c.Stop()

	blockStream := c.GetBlockProcessingStream(t)

	c.CreateCryptoForNs(t, types.NamespaceID(1), &signature.Profile{Scheme: signature.Ecdsa})
	ns1Policy := &protoblocktx.NamespacePolicy{
		Scheme:    signature.Ecdsa,
		PublicKey: c.GetPublicKey(types.NamespaceID(1)),
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
				"":                  protoblocktx.Status_ABORTED_MISSING_TXID,
				"missing signature": protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
				"missing namespace": protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION,
				"blind writes not allowed in ns lifecycle": protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
				"invalid namespace id in ns lifecycle":     protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
				"invalid policy in ns lifecycle":           protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID,
				"invalid signature":                        protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
				"duplicate namespace":                      protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
			},
		},
	}

	blk := &protoblocktx.Block{
		Number: 0,
		Txs:    tests[0].txs,
	}

	require.NoError(t, blockStream.Send(blk))

	cluster.ValidateStatus(t, tests[0].expectedTxStatus, blockStream)
}
