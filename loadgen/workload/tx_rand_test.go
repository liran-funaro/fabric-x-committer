/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package workload

import (
	"sync/atomic"
	"testing"

	"github.com/hyperledger/fabric-x-common/protoutil"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/api/servicepb"
	"github.com/hyperledger/fabric-x-committer/utils/signature"
)

// benchTx keeps the benchmarked result observable so the compiler cannot elide the work.
var benchTx *servicepb.LoadGenTx

// TestDerivePureAndUnique covers the derivation primitive that underlies keys, values, nonces, and
// metadata: its output is a pure function of the address (rootSeed, index) and the domain, and the
// size; the index and domain matter, the root seed is part of the contract, the domains separate the
// byte streams, and sizes expand exactly.
func TestDerivePureAndUnique(t *testing.T) {
	t.Parallel()
	p := newTxRandomProcess(&Profile{Seed: 42})

	k := p.derive(domainKey, 7, 0, 32)
	require.Len(t, k, 32)
	require.Equal(t, k, p.derive(domainKey, 7, 0, 32), "derive must be a pure function of its inputs")

	// The index and the root seed matter.
	require.NotEqual(t, k, p.derive(domainKey, 8, 0, 32))
	require.NotEqual(t, k, newTxRandomProcess(&Profile{Seed: 43}).derive(domainKey, 7, 0, 32),
		"the root seed is part of the contract")

	// Domain separation: key/value/nonce/metadata at the same index are independent streams.
	require.NotEqual(t, k, p.derive(domainValue, 7, 0, 32))
	require.NotEqual(t, k, p.derive(domainNonce, 7, 0, 32))
	require.NotEqual(t, p.derive(domainValue, 7, 0, 32), p.derive(domainMetadata, 7, 0, 32))

	// Size 0 => nil; size > 32 => MGF1-style expansion to the exact length.
	require.Nil(t, p.derive(domainKey, 7, 0, 0))
	require.Len(t, p.derive(domainKey, 7, 0, 100), 100)

	// The sub-index (used by value for the writing tx) separates the stream, so the same key derives
	// different value bytes per writing transaction; the single-index callers pass a zero sub-index.
	require.NotEqual(t, p.derive(domainValue, 7, 0, 32), p.derive(domainValue, 7, 1, 32),
		"a key written by different transactions must yield different values")
}

// TestTxRegenerableByIndex is the core property of index-addressable regeneration: a transaction's
// keys, values, and TX ID can be recomputed directly from its global tx-id, in arbitrary order and
// without generating the preceding transactions. The layout is a fixed per-run constant.
func TestTxRegenerableByIndex(t *testing.T) {
	t.Parallel()
	p := DefaultProfile(1)
	p.Transaction.ReadOnlyCount = 1
	p.Transaction.ReadWriteCount = 4
	p.Transaction.BlindWriteCount = 2
	p.Transaction.ReadWriteValueSize = 16
	p.Transaction.BlindWriteValueSize = 8

	gens := newIndependentTxGenerators(p, new(atomic.Uint64))
	require.Len(t, gens, 1)
	g := gens[0]

	const n = 20
	txs := make([]*servicepb.LoadGenTx, n)
	for i := range n {
		txs[i] = g.Next()
	}

	// A second, independent process regenerates arbitrary transactions purely by global tx-id.
	regen := newTxRandomProcess(p)
	creator := g.TxBuilder.EnvCreator
	for _, txIdx := range []uint64{0, 5, 19, 12, 5} {
		ns := txs[txIdx].Tx.Namespaces[0]
		keys := regen.slotKeys(txIdx)
		require.Len(t, ns.ReadsOnly, len(keys.readOnly))
		require.Len(t, ns.ReadWrites, len(keys.readWrite))
		require.Len(t, ns.BlindWrites, len(keys.blindWrite))

		for i := range ns.ReadsOnly {
			require.Equal(t, ns.ReadsOnly[i].Key, regen.key(keys.readOnly[i]))
		}
		for i := range ns.ReadWrites {
			require.Equal(t, ns.ReadWrites[i].Key, regen.key(keys.readWrite[i]))
			require.Equal(t, ns.ReadWrites[i].Value,
				regen.value(txIdx, keys.readWrite[i], p.Transaction.ReadWriteValueSize))
		}
		for i := range ns.BlindWrites {
			require.Equal(t, ns.BlindWrites[i].Key, regen.key(keys.blindWrite[i]))
			require.Equal(t, ns.BlindWrites[i].Value,
				regen.value(txIdx, keys.blindWrite[i], p.Transaction.BlindWriteValueSize))
		}

		wantID := protoutil.ComputeTxID(regen.nonce(txIdx), creator)
		require.Equal(t, wantID, txs[txIdx].Id)
	}
}

// TestSlotKeysSplit covers the enabled split with backward references only: within any transaction all
// slot key indices are distinct (creates never collide with references), read-write slots take the
// creates first, and every backward reference (the remaining write slots and all read-only slots) sits
// inside the working set [top-W, top) strictly below the committable frontier. Warmup references go to
// distinct NEGATIVE (pre-genesis) indices that never equal a non-negative create index.
func TestSlotKeysSplit(t *testing.T) {
	t.Parallel()
	p := DefaultProfile(1)
	p.Transaction.ReadOnlyCount = 1
	p.Transaction.ReadWriteCount = 2
	p.Transaction.BlindWriteCount = 1
	p.Transaction.NewKeysRate = ratePtr(2) // 2 of 3 write slots create => 1 backward write ref
	p.Transaction.ReferenceGap = 3
	p.Transaction.LookbackWindow = 8
	require.NoError(t, p.Transaction.Validate())

	proc := newTxRandomProcess(p)
	w := int64(p.Transaction.LookbackWindow) //nolint:gosec // test value fits int64.
	for txID := range uint64(30) {
		keys := proc.slotKeys(txID)
		all := make([]int64, 0, 4)
		all = append(all, keys.readOnly...)
		all = append(all, keys.readWrite...)
		all = append(all, keys.blindWrite...)

		seen := make(map[int64]struct{}, len(all))
		for _, idx := range all {
			_, dup := seen[idx]
			require.Falsef(t, dup, "tx %d has duplicate key index %d", txID, idx)
			seen[idx] = struct{}{}
		}

		committed := proc.committedFrontier(txID)
		require.Equal(t, committed, keys.readWrite[0])   // create wm=0 (read-write checked insert)
		require.Equal(t, committed+1, keys.readWrite[1]) // create wm=1 (read-write checked insert)

		// Both backward references (the blind write and the read-only) fall in the working set
		// [top-W, top), strictly below the frontier.
		top := proc.committedFrontier(txID - min(txID, p.Transaction.ReferenceGap))
		for _, ref := range []int64{keys.blindWrite[0], keys.readOnly[0]} {
			require.GreaterOrEqualf(t, ref, top-w, "tx %d ref %d below window", txID, ref)
			require.Lessf(t, ref, top, "tx %d ref %d not backward of top", txID, ref)
			require.Lessf(t, ref, committed, "tx %d ref %d not below the frontier", txID, ref)
		}
	}

	early := proc.slotKeys(0)
	require.Negative(t, early.blindWrite[0])
	require.Negative(t, early.readOnly[0])
	require.GreaterOrEqual(t, early.readWrite[0], int64(0))
	require.NotEqual(t, early.blindWrite[0], early.readOnly[0])
}

// TestSlotKeysStaticWorkingSet covers new-keys-rate 0: nothing is ever created, so every slot references
// the static pre-genesis (negative) working set [-W, 0). It also proves the per-transaction random
// offset SPREADS references across the whole window rather than pinning them to a single key.
func TestSlotKeysStaticWorkingSet(t *testing.T) {
	t.Parallel()
	p := DefaultProfile(1)
	p.Transaction.ReadWriteCount = 0
	p.Transaction.BlindWriteCount = 1
	p.Transaction.NewKeysRate = ratePtr(0) // no creates at all
	p.Transaction.ReferenceGap = 0
	p.Transaction.LookbackWindow = 16
	require.NoError(t, p.Transaction.Validate())

	proc := newTxRandomProcess(p)
	w := int64(p.Transaction.LookbackWindow) //nolint:gosec // test value fits int64.
	distinct := make(map[int64]struct{})
	for txID := range uint64(400) {
		ref := proc.slotKeys(txID).blindWrite[0]
		require.GreaterOrEqual(t, ref, -w) // inside [-W, 0)
		require.Negative(t, ref)           // never a create
		distinct[ref] = struct{}{}
	}
	// The random offset covers the window; a fixed gap alone would pin every reference to one key.
	require.Greater(t, len(distinct), int(w)/2)
}

// TestGeneratorStreamWorkerCountInvariant confirms the strengthened determinism contract: the SET of
// generated TX IDs (a pure function of the global tx-id multiset {0..N-1}) is identical regardless of
// worker count. Which worker emits which tx-id is scheduling-dependent, so we compare as sets.
func TestGeneratorStreamWorkerCountInvariant(t *testing.T) {
	t.Parallel()

	const total = 200
	idSet := func(workers uint32) map[string]struct{} {
		p := DefaultProfile(workers)
		p.Transaction.ReadWriteCount = 2
		counter := new(atomic.Uint64)
		gens := newIndependentTxGenerators(p, counter)
		ids := make(map[string]struct{}, total)
		for len(ids) < total {
			for _, g := range gens {
				ids[g.Next().Id] = struct{}{}
				if len(ids) >= total {
					break
				}
			}
		}
		return ids
	}

	require.Equal(t, idSet(1), idSet(4))
}

// BenchmarkTxNext measures full transaction generation per signature scheme. Signing dominates a
// real transaction by more than an order of magnitude, which is why the choice of derivation
// primitive for keys, values, the nonce, and metadata (all SHA-256 derive) is not a throughput
// concern — the derivation per TX is noise against the tens of µs of signing.
func BenchmarkTxNext(b *testing.B) {
	run := func(b *testing.B, scheme signature.Scheme) {
		b.Helper()
		p := DefaultProfile(1)
		p.Transaction.ReadOnlyCount = 2
		p.Transaction.ReadWriteCount = 2
		p.Transaction.BlindWriteCount = 1
		p.Policy.NamespacePolicies[DefaultGeneratedNamespaceID] = &Policy{Scheme: scheme}
		g := newIndependentTxGenerators(p, new(atomic.Uint64))[0]
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			benchTx = g.Next()
		}
	}
	b.Run("no-sign", func(b *testing.B) { run(b, signature.NoScheme) })
	b.Run("ecdsa", func(b *testing.B) { run(b, signature.Ecdsa) })
	b.Run("eddsa", func(b *testing.B) { run(b, signature.Eddsa) })
	b.Run("bls", func(b *testing.B) { run(b, signature.Bls) })
}
