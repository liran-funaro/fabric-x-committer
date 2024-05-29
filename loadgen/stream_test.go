package loadgen

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
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
				Scheme: signature.Ecdsa,
			},
		},
		Query: QueryProfile{
			KeySize:               32,
			QuerySize:             NewConstantDistribution(100),
			MinInvalidKeysPortion: NewConstantDistribution(0),
			Shuffle:               false,
			BufferSize:            workers * 100,
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
	c := StartBlockGenerator(defaultProfile(workers), NoLimit)

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
	c := StartTxGenerator(defaultProfile(workers), NoLimit)

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

func testWorkersProfiles() (profiles []*Profile) {
	for _, workers := range []uint32{1, 2, 4, 8} {
		profiles = append(profiles, defaultProfile(workers))
	}
	return profiles
}

func testTxProfiles() (profiles []*Profile) {
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
	for _, p := range testTxProfiles() {
		p := p
		onlyReadWrite := p.Transaction.ReadOnlyCount == nil
		t.Run(fmt.Sprintf("workers:%d-onlyReadWrite:%v", p.TxGenWorkers, onlyReadWrite), func(t *testing.T) {
			t.Parallel()
			c := StartTxGenerator(p, NoLimit)

			for i := 0; i < 100; i++ {
				requireValidTx(t, <-c.TxQueue, p, c.Signer)
			}
		})
	}
}

func TestGenValidBlock(t *testing.T) {
	t.Parallel()
	for _, p := range testTxProfiles() {
		p := p
		onlyReadWrite := p.Transaction.ReadOnlyCount == nil
		t.Run(fmt.Sprintf("workers:%d-onlyReadWrite:%v", p.TxGenWorkers, onlyReadWrite), func(t *testing.T) {
			t.Parallel()
			c := StartBlockGenerator(p, NoLimit)

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

	c := StartTxGenerator(p, NoLimit)
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
	p.Transaction.Signature.Scheme = signature.NoScheme
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

	c := StartTxGenerator(p, NoLimit)

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

func TestBlindWriteWithValue(t *testing.T) {
	t.Parallel()
	p := defaultProfile(1)
	p.Transaction.BlindWriteValueSize = 32
	p.Transaction.BlindWriteCount = NewConstantDistribution(2)

	c := StartTxGenerator(p, NoLimit)
	tx := <-c.TxQueue
	require.Len(t, tx.Namespaces[0].BlindWrites, 2)
	for _, v := range tx.Namespaces[0].BlindWrites {
		require.Len(t, v.Value, 32)
	}
}

func TestReadWriteWithValue(t *testing.T) {
	t.Parallel()
	p := defaultProfile(1)
	p.Transaction.ReadWriteValueSize = 32
	p.Transaction.ReadWriteCount = NewConstantDistribution(2)

	c := StartTxGenerator(p, NoLimit)
	tx := <-c.TxQueue
	require.Len(t, tx.Namespaces[0].ReadWrites, 2)
	for _, v := range tx.Namespaces[0].ReadWrites {
		require.Len(t, v.Value, 32)
	}
}

func TestGenTxWithRateLimit(t *testing.T) {
	t.Parallel()
	p := defaultProfile(8)
	limit := 100
	expectedSeconds := 3

	c := StartTxGenerator(p, LimiterConfig{InitialLimit: limit})
	start := time.Now()
	c.NextN(expectedSeconds * limit)
	duration := time.Since(start)
	require.InDelta(t, float64(expectedSeconds), duration.Seconds(), 0.1)
}

type queryTestEnv struct {
	p        *Profile
	keys     map[string]*struct{}
	blockGen *BlockStreamGenerator
	queryGen *QueryStreamGenerator
}

func newQueryTestEnv(p *Profile) *queryTestEnv {
	q := &queryTestEnv{
		p:        p,
		keys:     make(map[string]*struct{}),
		blockGen: StartBlockGenerator(p, NoLimit),
		queryGen: StartQueryGenerator(p, NoLimit),
	}
	for i := 0; i < 10; i++ {
		q.addBlock()
	}
	return q
}

func (q *queryTestEnv) addBlock() {
	block := <-q.blockGen.BlockQueue
	for _, tx := range block.Txs {
		for _, ns := range tx.Namespaces {
			for _, r := range ns.ReadsOnly {
				q.keys[string(r.Key)] = nil
			}
			for _, rw := range ns.ReadWrites {
				q.keys[string(rw.Key)] = nil
			}
			for _, w := range ns.BlindWrites {
				q.keys[string(w.Key)] = nil
			}
		}
	}
}

func (q *queryTestEnv) exists(key []byte) bool {
	_, ok := q.keys[string(key)]
	return ok
}

func (q *queryTestEnv) countExistingKeys(keys [][]byte) int {
	if q.keys == nil {
		return 0
	}
	c := 0
	for _, k := range keys {
		if q.exists(k) {
			c++
		}
	}
	return c
}

func TestQuery(t *testing.T) {
	t.Parallel()
	for _, p := range testWorkersProfiles() {
		p := p
		t.Run(fmt.Sprintf("workers:%d", p.TxGenWorkers), func(t *testing.T) {
			t.Parallel()
			env := newQueryTestEnv(p)

			for i := 0; i < 5; i++ {
				query := <-env.queryGen.QueryQueue
				// Since the blocks are generated in parallel, the order of the
				// keys in the block might not be the same as in the query.
				// So we need to consume blocks until we found all the keys.
				require.Eventually(t, func() bool {
					env.addBlock()
					return len(query.Namespaces[0].Keys) == env.countExistingKeys(query.Namespaces[0].Keys)
				}, time.Second*3, 1)
			}
		})
	}
}

func TestQueryWithInvalid(t *testing.T) {
	t.Parallel()
	for _, portion := range []float64{0.1, 0.5, 0.7} {
		portion := portion
		t.Run(fmt.Sprintf("invalid-portion:%.1f", portion), func(t *testing.T) {
			t.Parallel()
			p := defaultProfile(1)
			p.Query.MinInvalidKeysPortion = NewConstantDistribution(portion)
			env := newQueryTestEnv(p)

			existing := 0
			total := 0
			for i := 0; i < 10; i++ {
				query := <-env.queryGen.QueryQueue
				total += len(query.Namespaces[0].Keys)
				existing += env.countExistingKeys(query.Namespaces[0].Keys)
			}

			require.InDelta(t, portion, float64(total-existing)/float64(total), 1e-3)
		})
	}
}

func TestQueryShuffle(t *testing.T) {
	t.Parallel()
	portion := 0.5
	t.Run("no-shuffle", func(t *testing.T) {
		t.Parallel()
		p := defaultProfile(1)
		p.Query.MinInvalidKeysPortion = NewConstantDistribution(portion)
		p.Query.Shuffle = false
		env := newQueryTestEnv(p)

		for i := 0; i < 5; i++ {
			query := <-env.queryGen.QueryQueue
			validCount := int(math.Round(float64(len(query.Namespaces[0].Keys)) * portion))
			require.Equal(t, validCount, env.countExistingKeys(query.Namespaces[0].Keys[:validCount]))
			require.Equal(t, 0, env.countExistingKeys(query.Namespaces[0].Keys[validCount:]))
		}
	})

	t.Run("with-shuffle", func(t *testing.T) {
		t.Parallel()
		p := defaultProfile(1)
		p.Query.MinInvalidKeysPortion = NewConstantDistribution(portion)
		p.Query.Shuffle = true
		env := newQueryTestEnv(p)

		for i := 0; i < 5; i++ {
			query := <-env.queryGen.QueryQueue
			validCount := int(math.Round(float64(len(query.Namespaces[0].Keys)) * portion))
			require.Equal(t, validCount, env.countExistingKeys(query.Namespaces[0].Keys))
			require.NotEqual(t, validCount, env.countExistingKeys(query.Namespaces[0].Keys[:validCount]))
			require.NotEqual(t, 0, env.countExistingKeys(query.Namespaces[0].Keys[validCount:]))
		}
	})
}
