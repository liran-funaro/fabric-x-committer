package test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Percentage = float64

// Useful Percentage values.
var (
	Always  Percentage = 1
	Never   Percentage
	NoDelay = Constant(0)
)

type distributionType int

const (
	constant distributionType = iota
	normal
	uniform
	discrete
)

type Distribution = distributionHolder

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
	case discrete:
		d.Delegate = &discreteDistribution{Values: delegateValue["Values"].([]DiscreteValue)}
	default:
		panic("type not found")
	}
	return nil
}

func newConstantDistribution(value float64) Distribution {
	return distributionHolder{
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
	return distributionHolder{
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
	return distributionHolder{
		Type:     uniform,
		Delegate: &uniformDistribution{Min: min, Max: max},
	}
}

type DiscreteValue struct {
	Value       float64
	Probability Percentage
}

type discreteDistribution struct {
	Values []DiscreteValue
}

func Discrete(values []DiscreteValue) Distribution {
	return newDiscreteDistribution(values)
}

func newDiscreteDistribution(values []DiscreteValue) Distribution {
	return distributionHolder{
		Type:     discrete,
		Delegate: &discreteDistribution{Values: values},
	}
}

func (d *discreteDistribution) Generate() float64 {
	r := rand.Float64()
	remaining := float64(1)
	for _, value := range d.Values {
		remaining -= value.Probability
		if r >= remaining {
			return value.Value
		}
	}
	panic("map must have values within range (0, 1)")
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

func DistributionVar(p *Distribution, name string, defaultValue Distribution, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(input string) error {
		result, err := parseDistributionFlag(input)
		if err != nil {
			return err
		}
		*p = *result
		return nil
	})
}

func parseDistributionFlag(input string) (*Distribution, error) {
	flagValues := strings.Split(input, " ")
	if len(flagValues) < 2 {
		return nil, errors.New("insufficient arguments (valid examples: constant 1s, stable 5, uniform 1 10)")
	}
	distributionType := flagValues[0]
	values := make([]int64, len(flagValues)-1)
	for i, flagValue := range flagValues[1:] {
		value, err := parseValue(flagValue)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	result, err := parseDistribution(distributionType, values...)
	if err != nil {
		return nil, err
	}
	return result, nil
}

var durationFormat = regexp.MustCompile(`.*[a-z]+`)

func parseValue(value string) (int64, error) {
	if !durationFormat.MatchString(value) {
		return strconv.ParseInt(value, 10, 64)
	}
	duration, err := time.ParseDuration(value)
	return int64(duration), err
}

func parseDistribution(distributionType string, values ...int64) (*Distribution, error) {
	var result Distribution
	switch distributionType {
	case "constant":
		if len(values) != 1 {
			return nil, errors.New("only 1 value allowed")
		}
		result = Constant(values[0])
	case "stable":
		if len(values) != 1 {
			return nil, errors.New("only 1 value allowed")
		}
		result = Stable(values[0])
	case "volatile":
		if len(values) != 1 {
			return nil, errors.New("only 1 value allowed")
		}
		result = Volatile(values[0])
	case "uniform":
		if len(values) != 2 {
			return nil, errors.New("only 2 values allowed")
		}
		if values[0] >= values[1] {
			return nil, errors.New("wrong order: min < max")
		}
		result = Uniform(values[0], values[1])
	default:
		return nil, errors.New("distribution type not found")
	}
	return &result, nil
}
