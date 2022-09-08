package test

import "math/rand"

type Percentage = float64

var Always Percentage = 1
var Never Percentage = 0
var NoDelay = Constant(0)

type Distribution interface {
	Generate() float64
}

type ConstantDistribution struct{ Value float64 }

func (d *ConstantDistribution) Generate() float64 {
	return d.Value
}

type NormalDistribution struct{ Mean, Std float64 }

func (d *NormalDistribution) Generate() float64 {
	return rand.NormFloat64()*d.Std + d.Mean
}

type UniformDistribution struct{ Min, Max float64 }

func (d *UniformDistribution) Generate() float64 {
	return rand.Float64()*(d.Max-d.Min) + d.Min
}

var PercentageUniformDistribution = &UniformDistribution{Min: 0, Max: 1}

func Volatile(mean int64) *NormalDistribution {
	return &NormalDistribution{Mean: float64(mean), Std: float64(mean) / 2}
}
func Stable(mean int64) *NormalDistribution {
	return &NormalDistribution{Mean: float64(mean), Std: float64(mean) / 100}
}

func Constant(value int64) *ConstantDistribution {
	return &ConstantDistribution{Value: float64(value)}
}

func Uniform(min, max int64) *UniformDistribution {
	return &UniformDistribution{Min: float64(min), Max: float64(max)}
}
