package loadgen

import (
	"fmt"
	"math"
	"math/rand"
	"testing"

	"github.com/mitchellh/mapstructure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/constraints"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v3"
)

func makeRand() *rand.Rand {
	return rand.New(rand.NewSource(0))
}

func requireMarshalUnmarshal(t *testing.T, expected string, in *Distribution) {
	values := make(map[string]interface{})
	require.NoError(t, yaml.Unmarshal([]byte(expected), &values))
	out := &Distribution{}
	require.NoError(t, mapstructure.Decode(values, out))
	fmt.Println(out)

	rnd := makeRand()
	assert.Equal(t, in.MakeGenerator(rnd), out.MakeGenerator(rnd))
	assert.Equal(t, in.MakeIntGenerator(rnd), out.MakeIntGenerator(rnd))
	assert.Equal(t, in.MakePositiveIntGenerator(rnd), out.MakePositiveIntGenerator(rnd))
}

func requireAllInDelta[T any](t *testing.T, arr []T, expected T, delta float64) {
	for _, v := range arr {
		require.InDelta(t, expected, v, delta)
	}
}

func requireAllEqual[T any](t *testing.T, arr []T, expected T) {
	for _, v := range arr {
		require.Equal(t, expected, v)
	}
}

func TestConstDist(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		requireMarshalUnmarshal(t, "const: 0\ntype: constant\n", NewConstantDistribution(0))
	})

	for _, val := range []int{-1, 0, 1, 2, 10} {
		val := val
		t.Run(fmt.Sprintf("generate:%d", val), func(t *testing.T) {
			t.Parallel()
			d := NewConstantDistribution(float64(val))
			requireAllInDelta(t, GenerateArray(d.MakeGenerator(makeRand()), int(1e3)), float64(val), 1e-10)
			requireAllEqual(t, GenerateArray(d.MakeIntGenerator(makeRand()), int(1e3)), val)
			requireAllEqual(t, GenerateArray(d.MakePositiveIntGenerator(makeRand()), int(1e3)), Max(val, 1))
		})
	}
}

func TestNormDist(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		requireMarshalUnmarshal(t, "mean: 0\nstd: 1\ntype: normal\n", NewNormalDistribution(0, 1))
	})

	t.Run("generate", func(t *testing.T) {
		t.Parallel()
		g := NewNormalDistribution(0, 1).MakeGenerator(makeRand())
		values := GenerateArray(g, int(1e6))
		mean, std := MeanStd(values)
		require.InDelta(t, 0, mean, 1e-3)
		require.InDelta(t, 1, std, 1e-3)
	})
}

func TestUniformDist(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		requireMarshalUnmarshal(t, "max: 1\nmin: 0\ntype: uniform\n", NewUniformDistribution(0, 1))
	})

	t.Run("generate", func(t *testing.T) {
		t.Parallel()
		g := NewUniformDistribution(-1, 1).MakeGenerator(makeRand())
		values := GenerateArray(g, int(1e6))
		requireAllInDelta(t, values, 0, 1)
		mean, std := MeanStd(values)
		require.InDelta(t, 0, mean, 1e-3)
		// uniform std: sqrt( (1/12) * (b-a)^2 )
		require.InDelta(t, math.Sqrt(1./3.), std, 1e-3)
	})
}

func TestBernoulliDist(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		requireMarshalUnmarshal(t, "probability: 0.2\ntype: bernoulli\n", NewBernoulliDistribution(0.2))
	})

	t.Run("generate", func(t *testing.T) {
		t.Parallel()
		g := NewBernoulliDistribution(0.2).MakeGenerator(makeRand())
		values := GenerateArray(g, int(1e6))
		requireBernoulliDist(t, values, 0.2, 1e-3)
	})
}

func requireDiscreteDist[S, D constraints.Float | constraints.Integer](
	t *testing.T, sample []S, distValues []DiscreteValue[D], delta float64,
) {
	options := Map(distValues, func(_ int, v DiscreteValue[D]) int { return int(v.Value) })
	buckets := make([]uint64, len(options))
	for _, v := range sample {
		i := slices.Index(options, int(v))
		require.GreaterOrEqualf(t, i, 0, "invalid value: %v", v)
		buckets[i]++
	}

	var probabilitySum float64
	for i, v := range distValues {
		expectedProbability := Min(v.Probability, 1-probabilitySum)
		require.InDelta(t, expectedProbability, float64(buckets[i])/float64(len(sample)), delta)
		probabilitySum += expectedProbability
	}
}

func requireBernoulliDist(t *testing.T, sample []float64, probability Probability, delta float64) {
	requireDiscreteDist(t, sample, []DiscreteValue[float64]{
		{Value: 1, Probability: probability},
		{Value: 0, Probability: Always},
	}, delta)
}

func TestDiscreteDistMarshal(t *testing.T) {
	t.Parallel()

	t.Run("marshal", func(t *testing.T) {
		t.Parallel()
		requireMarshalUnmarshal(t,
			"type: discrete\nvalues:\n    - value: 0\n      probability: 1\n",
			NewDiscreteDistribution([]DiscreteValue[float64]{{Value: 0, Probability: 1}}),
		)
	})

	t.Run("generate", func(t *testing.T) {
		t.Parallel()
		distValues := []DiscreteValue[float64]{
			{Value: 0, Probability: 0.1},
			{Value: 1, Probability: 0.2},
			{Value: 2, Probability: Always},
		}
		g := NewDiscreteDistribution(distValues).MakeGenerator(makeRand())

		values := GenerateArray(g, int(1e6))
		requireDiscreteDist(t, values, distValues, 1e-3)
	})
}
