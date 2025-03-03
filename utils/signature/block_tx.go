package signature

import (
	"crypto/sha256"
	"encoding/asn1"

	"github.com/cockroachdb/errors"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// DigestTxNamespace digests a transactions for a given namespace index.
func DigestTxNamespace(tx *protoblocktx.Tx, nsIndex int) ([]byte, error) {
	if nsIndex >= len(tx.Namespaces) {
		return nil, errors.New("namespace index out of range")
	}
	ns := tx.Namespaces[nsIndex]
	n := Namespace{
		Reads:      make([]Reads, len(ns.ReadsOnly)),
		ReadWrites: make([]ReadWrites, len(ns.ReadWrites)),
		Writes:     make([]BlindWrites, len(ns.BlindWrites)),
	}

	for i, r := range ns.ReadsOnly {
		n.Reads[i] = Reads{
			Key:     r.Key,
			Version: r.Version,
		}
	}

	for i, rw := range ns.ReadWrites {
		n.ReadWrites[i] = ReadWrites{
			Key:     rw.Key,
			Version: rw.Version,
			Value:   rw.Value,
		}
	}

	for i, bw := range ns.BlindWrites {
		n.Writes[i] = BlindWrites{
			Key:   bw.Key,
			Value: bw.Value,
		}
	}

	bytes, err := asn1.Marshal(Tx{
		TxID:      tx.Id,
		Namespace: n,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal tx")
	}

	h := sha256.New()
	h.Write(bytes)
	return h.Sum(nil), nil
}

type Tx struct {
	TxID      string
	Namespace Namespace
}
type Namespace struct {
	Reads      []Reads
	ReadWrites []ReadWrites
	Writes     []BlindWrites
}
type Reads struct {
	Key     []byte
	Version []byte
}
type BlindWrites struct {
	Key   []byte
	Value []byte
}
type ReadWrites struct {
	Key     []byte
	Version []byte
	Value   []byte
}
