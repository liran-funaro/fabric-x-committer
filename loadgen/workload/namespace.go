package workload

import (
	"errors"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"google.golang.org/protobuf/proto"
)

// CreateNamespaces creating the transaction containing the requested namespaces into the MetaNamespace.
func CreateNamespaces(policy *PolicyProfile) (*protoblocktx.Tx, error) {
	txSigner := NewTxSignerVerifier(policy)
	policyNamespaceSigner, ok := txSigner.HashSigners[types.MetaNamespaceID]
	if !ok {
		return nil, errors.New("no policy namespace signer found; cannot create namespaces")
	}

	readWrites := make([]*protoblocktx.ReadWrite, 0, len(txSigner.HashSigners))
	for ns, p := range txSigner.HashSigners {
		if ns == types.MetaNamespaceID {
			continue
		}
		policyBytes, err := proto.Marshal(p.GetVerificationPolicy())
		if err != nil {
			return nil, errors.New("failed to serialize namespace policy")
		}
		readWrites = append(readWrites, &protoblocktx.ReadWrite{
			Key:   []byte(ns),
			Value: policyBytes,
		})
	}

	tx := &protoblocktx.Tx{
		Id: "initial policy update",
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:       types.MetaNamespaceID,
			NsVersion:  types.VersionNumber(0).Bytes(),
			ReadWrites: readWrites,
		}},
	}
	tx.Signatures = [][]byte{policyNamespaceSigner.Sign(tx, 0)}
	return tx, nil
}
