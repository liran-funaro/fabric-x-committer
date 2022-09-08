package test

import (
	"encoding/json"
	"strings"
)

type T = interface{}

const propertySeparator = "."

var valueSeparator = []byte(", ")
var dataPointSeparator = []byte("\n")

type BenchmarkIterator struct {
	baseConfig map[string]interface{}

	index    int
	property string
	values   []interface{}
}

func NewBenchmarkIterator(config T, prop string, values ...interface{}) *BenchmarkIterator {
	baseConfig, err := readBasicConfig(config)
	if err != nil {
		panic(err)
	}

	it := &BenchmarkIterator{
		baseConfig: baseConfig,
		property:   prop,
		values:     values,
	}
	if err != nil {
		panic(err)
	}

	return it
}

func (it *BenchmarkIterator) HasNext() bool {
	return it.index < len(it.values)
}

func (it *BenchmarkIterator) Next() {
	it.index++
}

func (it *BenchmarkIterator) Read(target T) error {
	it.setConfigValue(it.values[it.index])
	result, err := json.Marshal(it.baseConfig)
	if err != nil {
		return err
	}
	err = json.Unmarshal(result, target)
	if err != nil {
		return err
	}
	return nil
}

func (it *BenchmarkIterator) setConfigValue(value interface{}) {
	config := it.baseConfig

	props := strings.Split(it.property, propertySeparator)
	for i, prop := range props {
		isLast := i == len(props)-1
		if !isLast {
			config = config[prop].(map[string]interface{})
		} else {
			config[prop] = value
		}
	}
}

func readBasicConfig(basicConfig T) (map[string]interface{}, error) {
	marshalled, err := json.Marshal(basicConfig)
	if err != nil {
		return nil, err
	}

	var m map[string]interface{}
	err = json.Unmarshal(marshalled, &m)
	if err != nil {
		return nil, err
	}
	return m, nil
}
