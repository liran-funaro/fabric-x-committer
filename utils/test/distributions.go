package test

import "math/rand"

type Percentage = float64

type Distribution interface {
	Generate() float64
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
