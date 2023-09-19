package loadgen

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// result is used to prevent compiler optimizations.
var result float64

// printResult is used to prevent compiler optimizations.
func printResult() {
	fmt.Printf("Result: %v\n", result)
}

func defaultProfile(workers uint32) *Profile {
	return &Profile{
		Block: BlockProfile{
			Size:       100,
			BufferSize: 2,
		},
		Transaction: TransactionProfile{
			KeySize:        32,
			ReadWriteCount: NewConstantDistribution(2),
			BufferSize:     workers * 100,
			Signature: SignatureProfile{
				Scheme: Ecdsa,
			},
		},
		Conflicts: ConflictProfile{
			InvalidSignatures: Never,
		},
		Seed:                  249822374033311501,
		TxGenWorkers:          workers,
		TxDependenciesWorkers: workers,
		TxSignWorkers:         workers,
	}
}

func genericBlockBench(b *testing.B, workers uint32) {
	var sum float64

	b.ResetTimer()
	c := StartBlockGenerator(defaultProfile(workers))

	for i := 0; i < b.N; i++ {
		blk := <-c.BlockQueue
		sum += float64(blk.Number)
	}
	b.StopTimer()

	// Prevent compiler optimizations.
	result += sum
}

func genericTxBench(b *testing.B, workers uint32) {
	var sum float64

	b.ResetTimer()
	c := StartTxGenerator(defaultProfile(workers))

	for i := 0; i < b.N; i++ {
		tx := <-c.TxQueue
		sum += float64(len(tx.Namespaces))
	}
	b.StopTimer()

	// Prevent compiler optimizations.
	result += sum
}

func BenchmarkGenBlock(b *testing.B) {
	for _, workers := range []uint32{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			genericBlockBench(b, workers)
		})
	}
	printResult()
}

func BenchmarkGenTx(b *testing.B) {
	for _, workers := range []uint32{1, 2, 4, 8, 16, 32} {
		b.Run(fmt.Sprintf("workers_%d", workers), func(b *testing.B) {
			genericTxBench(b, workers)
		})
	}
	printResult()
}

func requireValidKey(t *testing.T, key []byte, profile *Profile) {
	require.Len(t, key, int(profile.Transaction.KeySize))
	require.Greater(t, SumInt(key), int64(0))
}

func requireValidTx(t *testing.T, tx *protoblocktx.Tx, profile *Profile, signer *TxSignerVerifier) {
	require.Len(t, tx.Namespaces, 1)

	if profile.Transaction.ReadOnlyCount != nil {
		require.Len(t, tx.Namespaces[0].ReadsOnly, 1)
	} else {
		require.Len(t, tx.Namespaces[0].ReadsOnly, 0)
	}

	if profile.Transaction.ReadWriteCount != nil {
		require.Len(t, tx.Namespaces[0].ReadWrites, 2)
	} else {
		require.Len(t, tx.Namespaces[0].ReadWrites, 0)
	}

	if profile.Transaction.BlindWriteCount != nil {
		require.Len(t, tx.Namespaces[0].BlindWrites, 3)
	} else {
		require.Len(t, tx.Namespaces[0].BlindWrites, 0)
	}

	for _, v := range tx.Namespaces[0].ReadsOnly {
		requireValidKey(t, v.Key, profile)
	}

	for _, v := range tx.Namespaces[0].ReadWrites {
		requireValidKey(t, v.Key, profile)
	}

	for _, v := range tx.Namespaces[0].BlindWrites {
		requireValidKey(t, v.Key, profile)
	}

	require.True(t, signer.Verify(tx))
}

func testProfiles() (profiles []*Profile) {
	for _, workers := range []uint32{1, 2, 4, 8} {
		for _, onlyReadWrite := range []bool{true, false} {
			p := defaultProfile(workers)
			if !onlyReadWrite {
				p.Transaction.ReadOnlyCount = NewConstantDistribution(1)
				p.Transaction.BlindWriteCount = NewConstantDistribution(3)
			} else {
				p.Transaction.ReadOnlyCount = nil
				p.Transaction.BlindWriteCount = nil
			}
			profiles = append(profiles, p)
		}
	}
	return profiles
}

func TestGenValidTx(t *testing.T) {
	t.Parallel()
	for _, p := range testProfiles() {
		p := p
		onlyReadWrite := p.Transaction.ReadOnlyCount == nil
		t.Run(fmt.Sprintf("workers:%d-onlyReadWrite:%v", p.TxGenWorkers, onlyReadWrite), func(t *testing.T) {
			t.Parallel()
			c := StartTxGenerator(p)

			require.IsType(t, &ecdsaSignerVerifier{}, c.Signer.HashSigner)

			for i := 0; i < 100; i++ {
				requireValidTx(t, <-c.TxQueue, p, c.Signer)
			}
		})
	}
}

func TestGenValidBlock(t *testing.T) {
	t.Parallel()
	for _, p := range testProfiles() {
		p := p
		onlyReadWrite := p.Transaction.ReadOnlyCount == nil
		t.Run(fmt.Sprintf("workers:%d-onlyReadWrite:%v", p.TxGenWorkers, onlyReadWrite), func(t *testing.T) {
			t.Parallel()
			c := StartBlockGenerator(p)

			require.IsType(t, &ecdsaSignerVerifier{}, c.Signer.HashSigner)

			for i := 0; i < 5; i++ {
				block := <-c.BlockQueue
				for _, tx := range block.Txs {
					requireValidTx(t, tx, p, c.Signer)
				}
			}
		})
	}
}

func TestGenInvalidSigTx(t *testing.T) {
	t.Parallel()
	p := defaultProfile(1)
	p.Conflicts.InvalidSignatures = 0.2

	c := StartTxGenerator(p)
	require.IsType(t, &ecdsaSignerVerifier{}, c.Signer.HashSigner)
	txs := c.NextN(1e3)
	valid := Map(txs, func(_ int, _ *protoblocktx.Tx) float64 {
		if !c.Signer.Verify(<-c.TxQueue) {
			return 1
		}
		return 0
	})
	requireBernoulliDist(t, valid, 0.2, 1e-2)
}

func TestGenDependentTx(t *testing.T) {
	t.Parallel()
	p := defaultProfile(1)
	p.Transaction.Signature.Scheme = NoScheme
	p.Conflicts.Dependencies = []DependencyDescription{
		{
			Gap:         NewConstantDistribution(1),
			Src:         "write",
			Dst:         "write",
			Probability: 0.1,
		},
		{
			Gap:         NewConstantDistribution(1),
			Src:         "read",
			Dst:         "write",
			Probability: 0.1,
		},
		{
			Gap:         NewConstantDistribution(1),
			Src:         "write",
			Dst:         "read-write",
			Probability: 0.1,
		},
		{
			Gap:         NewConstantDistribution(1),
			Src:         "read-write",
			Dst:         "read-write",
			Probability: 0.1,
		},
	}

	c := StartTxGenerator(p)

	txs := c.NextN(1e6)
	m := make(map[string]uint64)
	for _, tx := range txs {
		for _, ns := range tx.Namespaces {
			for _, r := range ns.ReadsOnly {
				m[string(r.Key)]++
			}
			for _, rw := range ns.ReadWrites {
				m[string(rw.Key)]++
			}
			for _, w := range ns.BlindWrites {
				m[string(w.Key)]++
			}
		}
	}

	var sum uint64
	for _, v := range m {
		sum += v - 1
	}
	require.InDelta(t, 0.4, float64(sum)/float64(len(txs)), 1e-3)
}
