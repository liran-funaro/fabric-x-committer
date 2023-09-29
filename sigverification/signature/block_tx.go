package signature

import (
	"crypto/sha256"
	"encoding/asn1"

	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

func HashTx(t *protoblocktx.Tx) []byte {

	tx := Tx{
		TxID:       t.Id,
		Namespaces: make([]Namespace, len(t.Namespaces)),
	}

	for i, ns := range t.GetNamespaces() {

		n := Namespace{
			Reads:      make([]Reads, len(ns.ReadsOnly)),
			ReadWrites: make([]ReadWrites, len(ns.GetReadWrites())),
			Writes:     make([]BlindWrites, len(ns.GetBlindWrites())),
		}

		for i, r := range ns.ReadsOnly {
			n.Reads[i] = Reads{
				Key:     r.GetKey(),
				Version: r.GetVersion(),
			}
		}

		for i, rw := range ns.ReadWrites {
			n.ReadWrites[i] = ReadWrites{
				Key:     rw.GetKey(),
				Version: rw.GetVersion(),
				Value:   rw.GetValue(),
			}
		}

		for i, bw := range ns.BlindWrites {
			n.Writes[i] = BlindWrites{
				Key:     bw.GetKey(),
				Version: bw.GetValue(),
				Value:   nil,
			}
		}

		tx.Namespaces[i] = n
	}

	bytes, err := asn1.Marshal(tx)
	if err != nil {
		// TODO better way to deal with an error here
		panic(errors.Wrap(err, "cannot hash transaction due to marshaling error"))
	}

	h := sha256.New()
	h.Write(bytes)
	return h.Sum(nil)
}

type Tx struct {
	TxID       string
	Namespaces []Namespace
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
	Key     []byte
	Version []byte
	Value   []byte
}

type ReadWrites struct {
	Key     []byte
	Version []byte
	Value   []byte
}
