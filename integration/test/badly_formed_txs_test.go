package test

import (
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/grpclog"
	"google.golang.org/protobuf/proto"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
)

func TestMain(m *testing.M) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, os.Stderr))
	os.Exit(m.Run())
}

func TestBadlyFormedTxs(t *testing.T) {
	RegisterTestingT(t)
	c := runner.NewCluster(
		t,
		&runner.Config{
			NumSigVerifiers: 2,
			NumVCService:    2,
			BlockSize:       5,
			BlockTimeout:    2 * time.Second,
		},
	)

	pubKey, _ := c.CreateCryptoForNs(types.NamespaceID(1), signature.Ecdsa)
	ns1Policy := &protoblocktx.NamespacePolicy{
		Scheme:    signature.Ecdsa,
		PublicKey: pubKey,
	}
	policyBytes, err := proto.Marshal(ns1Policy)
	require.NoError(t, err)

	tests := []struct {
		name            string
		txs             []*protoblocktx.Tx
		expectedResults *runner.ExpectedStatusInBlock
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
					Id: "no writes",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId:      1,
							NsVersion: types.NamespaceID(0).Bytes(),
							ReadsOnly: []*protoblocktx.Read{
								{
									Key:     []byte("k3"),
									Version: nil,
								},
							},
						},
					},
					Signatures: [][]byte{[]byte("signature")},
				},
			},
			expectedResults: &runner.ExpectedStatusInBlock{
				TxIDs: []string{"", "missing signature", "missing namespace", "no writes"},
				Statuses: []protoblocktx.Status{
					protoblocktx.Status_ABORTED_MISSING_TXID,
					protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
					protoblocktx.Status_ABORTED_MISSING_NAMESPACE_VERSION,
					protoblocktx.Status_ABORTED_NO_WRITES,
				},
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
			},
			expectedResults: &runner.ExpectedStatusInBlock{
				TxIDs: []string{
					"blind writes not allowed in ns lifecycle",
					"invalid namespace id in ns lifecycle",
					"invalid signature",
					"invalid policy in ns lifecycle",
				},
				Statuses: []protoblocktx.Status{
					protoblocktx.Status_ABORTED_BLIND_WRITES_NOT_ALLOWED,
					protoblocktx.Status_ABORTED_NAMESPACE_ID_INVALID,
					protoblocktx.Status_ABORTED_SIGNATURE_INVALID,
					protoblocktx.Status_ABORTED_NAMESPACE_POLICY_INVALID,
				},
			},
		},
		{
			name: "duplicate namespace and duplicate tx id",
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
					},
					Signatures: [][]byte{[]byte("signature")},
				},
				{
					Id: "missing signature",
					Namespaces: []*protoblocktx.TxNamespace{
						{
							NsId: 1,
						},
					},
				},
			},
			expectedResults: &runner.ExpectedStatusInBlock{
				TxIDs: []string{
					"duplicate namespace",
					"duplicate namespace",
					"missing signature",
				},
				Statuses: []protoblocktx.Status{
					protoblocktx.Status_ABORTED_DUPLICATE_NAMESPACE,
					protoblocktx.Status_ABORTED_DUPLICATE_TXID,
					protoblocktx.Status_ABORTED_DUPLICATE_TXID,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.SendTransactionsToOrderer(t, tt.txs)
			c.ValidateExpectedResultsInCommittedBlock(t, tt.expectedResults)
		})
	}
	fmt.Println("done")
}
