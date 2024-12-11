package workload

import (
	"errors"
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"google.golang.org/protobuf/proto"
)

// NamespaceGenerator generates a transaction containing the namespaces to create in the DB.
// it assumes that a Meta-Namespace was already created.
type NamespaceGenerator struct {
	namespaces []types.NamespaceID
	scheme     Scheme
	sigProfile SignatureProfile
}

// NewNamespaceGenerator creates a namespace generator struct.
func NewNamespaceGenerator(profile SignatureProfile) *NamespaceGenerator {
	return &NamespaceGenerator{
		namespaces: profile.Namespaces,
		scheme:     profile.Scheme,
		sigProfile: profile,
	}
}

// CreateNamespaces creating the transaction containing the requested namespaces into the MetaNamespace.
func (nsg *NamespaceGenerator) CreateNamespaces() (*protoblocktx.Tx, error) {
	txSigner := NewTxSignerVerifier(&nsg.sigProfile)
	scheme, publicKey := txSigner.HashSigner.scheme, txSigner.HashSigner.pubKey
	nsPolicy := &protoblocktx.NamespacePolicy{
		Scheme:    scheme,
		PublicKey: publicKey,
	}
	policyBytes, err := proto.Marshal(nsPolicy)
	if err != nil {
		return nil, errors.New("failed to serialize namespace policy")
	}

	writeToMetaNs := &protoblocktx.TxNamespace{
		NsId:      uint32(types.MetaNamespaceID),
		NsVersion: types.VersionNumber(0).Bytes(),
	}
	for _, nsID := range nsg.namespaces {
		writeToMetaNs.ReadWrites = append(writeToMetaNs.ReadWrites, &protoblocktx.ReadWrite{
			Key:   nsID.Bytes(),
			Value: policyBytes,
		})
	}

	rnd := UUIDGenerator{Rnd: rand.New(rand.NewSource(nsg.sigProfile.Seed))}
	tx := &protoblocktx.Tx{
		Id:         rnd.Next(),
		Namespaces: []*protoblocktx.TxNamespace{writeToMetaNs},
	}

	for idx := range tx.Namespaces {
		tx.Signatures = append(tx.Signatures, txSigner.HashSigner.Sign(tx, idx))
	}
	return tx, nil
}
