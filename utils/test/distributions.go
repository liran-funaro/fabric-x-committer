package test

import (
	"bytes"
	"encoding/json"
	"math/rand"
)

type Percentage = float64

var Always Percentage = 1
var Never Percentage = 0
var NoDelay = Constant(0)

type distributionType int

const (
	constant distributionType = iota
	normal
	uniform
)

type Distribution = *distributionHolder

type distribution interface {
	Generate() float64
}

type distributionHolder struct {
	Type     distributionType
	Delegate distribution
}

func (d *distributionHolder) Generate() float64 {
	return d.Delegate.Generate()
}

func (d *distributionHolder) UnmarshalJSON(b []byte) error {
	holder := map[string]interface{}{}
	err := json.NewDecoder(bytes.NewReader(b)).Decode(&holder)
	if err != nil {
		return err
	}
	d.Type = distributionType(holder["Type"].(float64))
	delegateValue := holder["Delegate"].(map[string]interface{})
	switch d.Type {
	case uniform:
		d.Delegate = &uniformDistribution{Min: delegateValue["Min"].(float64), Max: delegateValue["Max"].(float64)}
	case constant:
		d.Delegate = &constantDistribution{Value: delegateValue["Value"].(float64)}
	case normal:
		d.Delegate = &normalDistribution{Mean: delegateValue["Mean"].(float64), Std: delegateValue["Std"].(float64)}
	default:
		panic("type not found")
	}
	return nil
}

func newConstantDistribution(value float64) Distribution {
	return &distributionHolder{
		Type:     constant,
		Delegate: &constantDistribution{Value: value},
	}
}

type constantDistribution struct {
	Value float64
}

func (d *constantDistribution) Generate() float64 {
	return d.Value
}

func newNormalDistribution(mean, std float64) Distribution {
	return &distributionHolder{
		Type:     normal,
		Delegate: &normalDistribution{Mean: mean, Std: std},
	}
}

type normalDistribution struct {
	Mean, Std float64
}

func (d *normalDistribution) Generate() float64 {
	return rand.NormFloat64()*d.Std + d.Mean
}

func newUniformDistribution(min, max float64) Distribution {
	return &distributionHolder{
		Type:     uniform,
		Delegate: &uniformDistribution{Min: min, Max: max},
	}
}

type uniformDistribution struct {
	Min, Max float64
}

func (d *uniformDistribution) Generate() float64 {
	return rand.Float64()*(d.Max-d.Min) + d.Min
}

var PercentageUniformDistribution = newUniformDistribution(0, 1)

func Volatile(mean int64) Distribution {
	return newNormalDistribution(float64(mean), float64(mean)/2)
}
func Stable(mean int64) Distribution {
	return newNormalDistribution(float64(mean), float64(mean)/100)
}

func Constant(value int64) Distribution {
	return newConstantDistribution(float64(value))
}

func Uniform(min, max int64) Distribution {
	return newUniformDistribution(float64(min), float64(max))
}
