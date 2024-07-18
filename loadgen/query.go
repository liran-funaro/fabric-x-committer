package loadgen

import (
	"math"
	"math/rand"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
)

// QueryGenerator generates a new query for keys.
type QueryGenerator struct {
	ValidKeyGenerator   Generator[[]byte]
	InvalidKeyGenerator Generator[[]byte]
	Size                Generator[int]
	InvalidPortion      Generator[float64]
	ShuffleRnd          *rand.Rand
	Shuffle             bool
}

// newIndependentTxGenerator creates a new QueryGenerator.
func newQueryGenerator(rnd *rand.Rand, keys Generator[Key], profile *Profile) *QueryGenerator {
	return &QueryGenerator{
		ValidKeyGenerator:   keys,
		InvalidKeyGenerator: &ByteArrayGenerator{Size: profile.Key.Size, Rnd: rnd},
		Size:                profile.Query.QuerySize.MakeIntGenerator(rnd),
		InvalidPortion:      profile.Query.MinInvalidKeysPortion.MakeGenerator(rnd),
		ShuffleRnd:          rnd,
		Shuffle:             profile.Query.Shuffle,
	}
}

// Next generate a new query.
func (g *QueryGenerator) Next() *protoqueryservice.Query {
	size := g.Size.Next()
	keys := make([][]byte, 0, size)

	invalidSize := Clip(int(math.Round(g.InvalidPortion.Next()*float64(size))), 0, size)
	keys = append(keys, GenerateArray(g.ValidKeyGenerator, size-invalidSize)...)
	keys = append(keys, GenerateArray(g.InvalidKeyGenerator, invalidSize)...)

	if g.Shuffle {
		g.ShuffleRnd.Shuffle(size, func(i, j int) {
			keys[i], keys[j] = keys[j], keys[i]
		})
	}

	return &protoqueryservice.Query{
		Namespaces: []*protoqueryservice.QueryNamespace{
			{
				NsId: 0,
				Keys: keys,
			},
		},
	}
}
