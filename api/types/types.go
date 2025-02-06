package types

import (
	"errors"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"google.golang.org/protobuf/encoding/protowire"
)

type (
	// NamespaceID identities a database namespace.
	NamespaceID uint32
	// VersionNumber represents a row's version.
	VersionNumber uint64
)

// MetaNamespaceID is an ID of a system namespace which holds information about user's namespaces.
const MetaNamespaceID = NamespaceID(1024)

// ErrInvalidNamespaceID is returned when the namespace ID cannot be parsed.
var ErrInvalidNamespaceID = errors.New("invalid namespace ID")

// Bytes converts a NamespaceID to bytes representation.
func (nsID NamespaceID) Bytes() []byte {
	return protowire.AppendVarint(nil, uint64(nsID))
}

// NamespaceIDFromBytes converts a bytes representation of NamespaceID to NamespaceID.
func NamespaceIDFromBytes(ns []byte) (NamespaceID, error) {
	v, l := protowire.ConsumeVarint(ns)
	if l < 0 || l != len(ns) {
		return 0, ErrInvalidNamespaceID
	}
	return NamespaceID(v), nil
}

// Bytes converts a version number representation to bytes representation.
func (v VersionNumber) Bytes() []byte {
	return protowire.AppendVarint(nil, uint64(v))
}

// VersionNumberFromBytes converts a version bytes representation to a number representation.
func VersionNumberFromBytes(version []byte) VersionNumber {
	v, _ := protowire.ConsumeVarint(version)
	return VersionNumber(v)
}

// CreateStatusWithHeight creates a protoblocktx.StatusWithHeight with the given values.
func CreateStatusWithHeight(s protoblocktx.Status, blkNum uint64, txNum int) *protoblocktx.StatusWithHeight {
	return &protoblocktx.StatusWithHeight{
		Code:        s,
		BlockNumber: blkNum,
		TxNumber:    uint32(txNum), //nolint:gosec
	}
}
