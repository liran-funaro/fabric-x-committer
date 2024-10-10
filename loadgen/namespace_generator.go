package loadgen

import (
	"errors"

	"github.com/google/uuid"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"google.golang.org/protobuf/proto"
)

// NamespaceGenerator generates a transaction containing the namespaces to create in the DB.
// it assumes that a Meta-Namespace was already created.
type NamespaceGenerator struct {
	namespaces         []types.NamespaceID
	scheme             Scheme
	sigProfile         SignatureProfile
	numberOfBlocksSent uint64
	vcInit             bool
}

// NewNamespaceGenerator creates a namespace generator struct.
func NewNamespaceGenerator(profile SignatureProfile) *NamespaceGenerator {
	nsGen := &NamespaceGenerator{
		namespaces:         profile.Namespaces,
		scheme:             profile.Scheme,
		sigProfile:         profile,
		numberOfBlocksSent: 0,
		vcInit:             false,
	}
	logger.Infof(
		"Namespace generator initialized with %d namespaces. - namespaces: %v - scheme chosen: %v",
		len(nsGen.namespaces),
		nsGen.namespaces,
		nsGen.scheme,
	)
	return nsGen
}

// getSigner generates new signer.
func (nsg *NamespaceGenerator) getSigner() *TxSignerVerifier {
	return NewTxSignerVerifier(&nsg.sigProfile)
}

// CreateNamespaces creating the transaction containing the requested namespaces into the MetaNamespace.
func (nsg *NamespaceGenerator) CreateNamespaces() (*protoblocktx.Tx, error) {
	writeToMetaNs := &protoblocktx.TxNamespace{
		NsId:       uint32(types.MetaNamespaceID),
		NsVersion:  types.VersionNumber(0).Bytes(),
		ReadWrites: make([]*protoblocktx.ReadWrite, 0, len(nsg.namespaces)),
	}

	for _, nsID := range nsg.namespaces {
		nsSigner := nsg.getSigner()
		scheme, publicKey := nsSigner.HashSigner.scheme, nsSigner.HashSigner.pubKey
		nsPolicy := createNamespacePolicy(scheme, publicKey)
		policyBytes, err := protoToBytes(nsPolicy)
		if err != nil {
			return nil, errors.New("failed to serialize namespace policy")
		}

		writeToMetaNs.ReadWrites = append(writeToMetaNs.ReadWrites, &protoblocktx.ReadWrite{
			Key:   nsID.Bytes(),
			Value: policyBytes,
		})
	}

	tx := &protoblocktx.Tx{
		Id: uuid.New().String(),
		Namespaces: []*protoblocktx.TxNamespace{
			writeToMetaNs,
		},
	}

	nsg.AddSignatures(tx)
	return tx, nil
}

// AddSignatures adds signature for each namespace in a given transaction.
func (nsg *NamespaceGenerator) AddSignatures(tx *protoblocktx.Tx) {
	for idx := range tx.Namespaces {
		txSigner := nsg.getSigner()
		signNs := txSigner.HashSigner.Sign(tx, idx)
		tx.Signatures = append(tx.Signatures, signNs)
	}
}

// Next is implementing the generator interface method.
func (nsg *NamespaceGenerator) Next() *protoblocktx.Block {
	if nsg.numberOfBlocksSent > 0 {
		return nil
	}

	nsTx, err := nsg.CreateNamespaces()
	if err != nil {
		logger.Panic(errors.Join(err, errors.New("failed to create namespace tx")))
	}
	blk := toBlock(nsTx, nsg.numberOfBlocksSent)
	nsg.numberOfBlocksSent++
	return blk
}

func toBlock(tx *protoblocktx.Tx, blkNumber uint64) *protoblocktx.Block {
	block := &protoblocktx.Block{
		Number: blkNumber,
		Txs:    []*protoblocktx.Tx{tx},
	}

	return block
}

func createNamespacePolicy(scheme Scheme, publicKey []byte) *protoblocktx.NamespacePolicy {
	return &protoblocktx.NamespacePolicy{
		Scheme:    scheme,
		PublicKey: publicKey,
	}
}

func protoToBytes(msg proto.Message) ([]byte, error) {
	return proto.Marshal(msg)
}
