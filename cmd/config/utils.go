package config

import (
	"os"
	"path/filepath"
	"regexp"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"

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
	content, err := utils.ReadFile(filePath)
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

func FilePaths(dir string, filePattern *regexp.Regexp) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	absoluteDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var filePaths []string
	for _, entry := range entries {
		if !entry.IsDir() && filePattern.MatchString(entry.Name()) {
			filePaths = append(filePaths, filepath.Join(absoluteDir, entry.Name()))
		}
	}
	return filePaths, nil
}
