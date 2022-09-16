package config

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type configMap = map[interface{}]interface{}

func MergeYamlConfigs(filenames ...string) ([]byte, error) {
	result := make(configMap)
	for _, filename := range filenames {
		current, err := readYamlConfig(filename)
		if err != nil {
			return nil, err
		}
		result = reduceMerge(result, current)
	}

	return yaml.Marshal(result)
}

func readYamlConfig(filename string) (configMap, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	content, err := ioutil.ReadAll(file)
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
	var filePaths []string
	for _, entry := range entries {
		if !entry.IsDir() && filePattern.MatchString(entry.Name()) {
			filePaths = append(filePaths, filepath.Join(dir, entry.Name()))
		}
	}
	return filePaths, nil
}
