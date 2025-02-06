package types

import (
	"errors"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"google.golang.org/protobuf/encoding/protowire"
)

type (
	// VersionNumber represents a row's version.
	VersionNumber uint64
)

// MetaNamespaceID is an ID of a system namespace which holds information about user's namespaces.
const MetaNamespaceID = "_meta"

// ErrInvalidNamespaceID is returned when the namespace ID cannot be parsed.
var ErrInvalidNamespaceID = errors.New("invalid namespace ID")

// MaxNamespaceIDLength defines the maximum number of characters allowed for namespace IDs.
const MaxNamespaceIDLength = 64

// ValidateNamespaceID checks that a given namespace fulfills namespace naming conventions.
func ValidateNamespaceID(nsID string) error {
	// length checks
	if len(nsID) == 0 || len(nsID) > MaxNamespaceIDLength {
		return ErrInvalidNamespaceID
	}

	// if it matches our holy MetaNamespaceID it is valid
	if nsID == MetaNamespaceID {
		return nil
	}

	// TODO: add more validation checks

	return nil
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
