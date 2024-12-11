package workload

import (
	"fmt"
	"math/rand"
)

// Probability is a float in the closed interval [0,1].
type Probability = float64

type DistributionType string

const (
	constant  DistributionType = "constant"
	uniform   DistributionType = "uniform"
	normal    DistributionType = "normal"
	bernoulli DistributionType = "bernoulli"
	discrete  DistributionType = "discrete"
)

const (
	// Always is 100%.
	Always Probability = 1
	// Never is 0%.
	Never Probability = 0
)

// Distribution is a descriptor of a distribution.
type Distribution struct {
	// DistributionType is the type of the distribution.
	Type DistributionType `mapstructure:"type"`

	Const float64 `mapstructure:"const"`

	Min float64 `mapstructure:"min"`
	Max float64 `mapstructure:"max"`

	Mean float64 `mapstructure:"mean"`
	Std  float64 `mapstructure:"std"`

	Probability Probability `mapstructure:"probability"`

	Values []DiscreteValue[float64] `mapstructure:"values"`
}

// MakeGenerator returns a new generator according to the distribution description.
func (d *Distribution) MakeGenerator(rnd *rand.Rand) Generator[float64] {
	switch d.Type {
	case constant:
		return &ConstGenerator[float64]{
			Const: d.Const,
		}
	case uniform:
		return &UniformGenerator{
			Rnd: rnd,
			Min: d.Min,
			Max: d.Max,
		}
	case normal:
		return &NormalGenerator{
			Rnd:  rnd,
			Mean: d.Mean,
			Std:  d.Std,
		}
	case bernoulli:
		return &BernoulliGenerator{
			Rnd:         rnd,
			Probability: d.Probability,
		}
	case discrete:
		return &DiscreteGenerator[float64]{
			Rnd:    rnd,
			Values: d.Values,
		}
	default:
		panic(fmt.Sprintf("unsupported distribution: %s", d.Type))
	}
}

// MakeIntGenerator returns a new integer generator according to the distribution description.
func (d *Distribution) MakeIntGenerator(rnd *rand.Rand) Generator[int] {
	return &FloatToIntGenerator{FloatGen: d.MakeGenerator(rnd)}
}

// MakePositiveIntGenerator returns a new positive (>=1) integer generator according to the distribution description.
func (d *Distribution) MakePositiveIntGenerator(rnd *rand.Rand) Generator[int] {
	return &FloatToPositiveIntGenerator{FloatGen: d.MakeGenerator(rnd)}
}

// MakeBooleanGenerator returns a new boolean generator according to the distribution description.
func (d *Distribution) MakeBooleanGenerator(rnd *rand.Rand) Generator[bool] {
	return &FloatToBooleanGenerator{FloatGen: d.MakeGenerator(rnd)}
}

// NewConstantDistribution creates a constant value distribution.
func NewConstantDistribution(value float64) *Distribution {
	return &Distribution{
		Type:  constant,
		Const: value,
	}
}

// NewNormalDistribution creates a normal distribution.
func NewNormalDistribution(mean, std float64) *Distribution {
	return &Distribution{
		Type: normal,
		Mean: mean,
		Std:  std,
	}
}

// NewUniformDistribution creates a uniform distribution.
func NewUniformDistribution(min, max float64) *Distribution {
	return &Distribution{
		Type: uniform,
		Min:  min,
		Max:  max,
	}
}

// NewDiscreteDistribution creates a discrete distribution.
func NewDiscreteDistribution(values []DiscreteValue[float64]) *Distribution {
	return &Distribution{
		Type:   discrete,
		Values: values,
	}
}

// NewBernoulliDistribution creates a Bernoulli distribution.
func NewBernoulliDistribution(probability Probability) *Distribution {
	return &Distribution{
		Type:        bernoulli,
		Probability: probability,
	}
}

// UniformGenerator generates values with a uniform distribution.
type UniformGenerator struct {
	Rnd      *rand.Rand
	Min, Max float64
}

// Next yields the next uniform value.
func (d *UniformGenerator) Next() float64 {
	return d.Rnd.Float64()*(d.Max-d.Min) + d.Min
}

// NormalGenerator generates values with a normal distribution.
type NormalGenerator struct {
	Rnd       *rand.Rand
	Mean, Std float64
}

// Next yields the next normal value.
func (d *NormalGenerator) Next() float64 {
	return d.Rnd.NormFloat64()*d.Std + d.Mean
}

// BernoulliGenerator generates 1 with probability of p.
type BernoulliGenerator struct {
	Rnd         *rand.Rand
	Probability Probability
}

// Next yields the next bernoulli value.
func (d *BernoulliGenerator) Next() float64 {
	if d.Rnd.Float64() < d.Probability {
		return 1
	}
	return 0
}

// DiscreteValue describe the appearance probability of a value.
type DiscreteValue[T any] struct {
	Value       T           `mapstructure:"value"`
	Probability Probability `mapstructure:"probability"`
}

// DiscreteGenerator generates values with a discrete distribution.
type DiscreteGenerator[T any] struct {
	Rnd    *rand.Rand
	Values []DiscreteValue[T]
}

// Next yields the next discrete value.
func (d *DiscreteGenerator[T]) Next() T {
	r := d.Rnd.Float64()
	remaining := float64(1)
	for _, value := range d.Values {
		remaining -= value.Probability
		if r >= remaining {
			return value.Value
		}
	}
	panic("probabilities must sum to 1")
}
