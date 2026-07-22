/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package workload

import (
	"sync/atomic"

	"github.com/hyperledger/fabric-x-common/api/applicationpb"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils"
	"github.com/hyperledger/fabric-x-committer/utils/testsig"
)

// invalidSignatureBytes is the dummy signature stamped on transactions selected to be invalid.
var invalidSignatureBytes = []byte("dummy")

type (
	// IndependentTxGenerator builds transactions from a txRandomProcess. The generator only assembles:
	// it embeds the TransactionProfile (the fixed layout set sizes and value sizes), lays out the
	// namespace, optionally stamps a dummy endorsement in place of a real signature, and hands off to
	// the TxBuilder. The random content — keys, values, nonce/TX ID, and metadata — comes from the
	// process and is a pure function of the transaction index, so a transaction is reproducible by
	// index.
	IndependentTxGenerator struct {
		TransactionProfile
		process   *txRandomProcess
		txCounter *atomic.Uint64
		TxBuilder *TxBuilder
	}

	// Key is an alias for byte array.
	Key = []byte
)

// DefaultGeneratedNamespaceID for now we're only generating transactions for a single namespace.
const DefaultGeneratedNamespaceID = "0"

// newIndependentTxGenerators creates one generator per worker, each with its own txRandomProcess (each
// process is driven from a single goroutine and reuses a seed buffer), all sharing one global tx-id
// counter. Worker-count invariance comes from the shared counter, not from sharing the process.
func newIndependentTxGenerators(profile *Profile, counter *atomic.Uint64) []*IndependentTxGenerator {
	utils.Must(profile.Transaction.Validate())
	gens := make([]*IndependentTxGenerator, profile.Workers)
	for i := range gens {
		txb, err := NewTxBuilderFromPolicy(&profile.Policy, nil)
		utils.Must(err)
		gens[i] = &IndependentTxGenerator{
			process:            newTxRandomProcess(profile),
			txCounter:          counter,
			TxBuilder:          txb,
			TransactionProfile: profile.Transaction,
		}
	}
	return gens
}

// Next generates a new TX. It grabs the next global transaction index and assembles the transaction
// from the process: keys and their values come from the per-transaction slot layout (new creates +
// existing references, keyed by flat key index), while the nonce and metadata are addressed by the
// transaction index, so the whole TX is a pure function of that index.
func (g *IndependentTxGenerator) Next() *servicepb.LoadGenTx {
	txIdx := g.txCounter.Add(1) - 1
	keys := g.process.slotKeys(txIdx)

	ns := &applicationpb.TxNamespace{
		NsId:        DefaultGeneratedNamespaceID,
		NsVersion:   0,
		ReadsOnly:   make([]*applicationpb.Read, g.ReadOnlyCount),
		ReadWrites:  make([]*applicationpb.ReadWrite, g.ReadWriteCount),
		BlindWrites: make([]*applicationpb.Write, g.BlindWriteCount),
	}

	// Keys come from the flat key indices in `keys`; each write-site's value is keyed by that key index
	// AND this transaction's index, so an update to a key writes a value distinct from the create's.
	for i := range ns.ReadsOnly {
		ns.ReadsOnly[i] = &applicationpb.Read{Key: g.process.key(keys.readOnly[i])}
	}
	for i := range ns.ReadWrites {
		ns.ReadWrites[i] = &applicationpb.ReadWrite{
			Key:   g.process.key(keys.readWrite[i]),
			Value: g.process.value(txIdx, keys.readWrite[i], g.ReadWriteValueSize),
		}
	}
	for i := range ns.BlindWrites {
		ns.BlindWrites[i] = &applicationpb.Write{
			Key:   g.process.key(keys.blindWrite[i]),
			Value: g.process.value(txIdx, keys.blindWrite[i], g.BlindWriteValueSize),
		}
	}

	var metadata [][]byte
	if item := g.process.metadata(txIdx, g.MetadataSize); len(item) > 0 {
		metadata = [][]byte{item}
	}

	tx := &applicationpb.Tx{
		Namespaces: []*applicationpb.TxNamespace{ns},
		Metadata:   metadata,
	}
	if g.process.invalidSignature(txIdx) {
		// Pre-assigning prevents TxBuilder from re-signing the TX.
		tx.Endorsements = make([]*applicationpb.Endorsements, len(tx.Namespaces))
		for i := range tx.Namespaces {
			tx.Endorsements[i] = testsig.CreateEndorsementsForThresholdRule(invalidSignatureBytes)[0]
		}
	}
	return g.TxBuilder.MakeTxWithNonce(g.process.nonce(txIdx), tx)
}
