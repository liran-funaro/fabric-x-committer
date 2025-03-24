package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type configMap = map[interface{}]interface{}

func MergeYamlConfigs(filePaths ...string) ([]byte, error) {
	result := make(configMap)
	for _, filePath := range filePaths {
		current, err := readYamlConfig(filePath)
		if err != nil {
			return nil, err
		}
		result = reduceMerge(result, current)
	}

	return yaml.Marshal(result)
}

func readYamlConfig(filePath string) (configMap, error) {
	content, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	var current configMap
	err = yaml.Unmarshal(content, &current)
	if err != nil {
		return nil, err
	}
	return current, nil
}

func reduceMerge(accumulator, current configMap) configMap {
	for key, currentValue := range current {
		if currentValue, ok := currentValue.(configMap); ok {
			if accumulatorValue, ok := accumulator[key]; ok {
				if accumulatorValue, ok := accumulatorValue.(configMap); ok {
					accumulator[key] = reduceMerge(accumulatorValue, currentValue)
					continue
				}
			}
		}
		accumulator[key] = currentValue
	}
	return accumulator
}
