package types

import "google.golang.org/protobuf/encoding/protowire"

type (
	// NamespaceID identities a database namespace.
	NamespaceID uint32
	// VersionNumber represents a row's version.
	VersionNumber uint64
)

// MetaNamespaceID is an ID of a system namespace which holds information about user's namespaces.
const MetaNamespaceID = NamespaceID(1024)

// Bytes converts a NamespaceID to bytes representation.
func (nsID NamespaceID) Bytes() []byte {
	return protowire.AppendVarint(nil, uint64(nsID))
}

// NamespaceIDFromBytes converts a bytes representation of NamespaceID to NamespaceID.
func NamespaceIDFromBytes(ns []byte) NamespaceID {
	v, _ := protowire.ConsumeVarint(ns)
	return NamespaceID(v)
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
