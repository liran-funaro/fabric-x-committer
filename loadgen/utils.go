package loadgen

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"golang.org/x/exp/constraints"
	"gopkg.in/yaml.v3"
)

// Max returns the maximal value of a given list of items.
func Max[T constraints.Ordered](a T, others ...T) T {
	for _, v := range others {
		if v > a {
			a = v
		}
	}
	return a
}

// Min returns the minimal value of a given list of items.
func Min[T constraints.Ordered](a T, others ...T) T {
	for _, v := range others {
		if v < a {
			a = v
		}
	}
	return a
}

// SumInt returns the sum of an array of integers.
func SumInt[T constraints.Integer](arr []T) int64 {
	var sum int64
	for _, v := range arr {
		sum += int64(v)
	}
	return sum
}

// Sum returns the sum of an array of floats.
func Sum[T constraints.Float](arr []T) float64 {
	var sum float64
	for _, v := range arr {
		sum += float64(v)
	}
	return sum
}

// MeanStd returns the mean and std of an array of float samples.
func MeanStd[T constraints.Float](arr []T) (mean, std float64) {
	n := float64(len(arr))
	mean = Sum(arr) / n

	// Sample std: sqrt( Σ (v - mean)^2 / (n-1) )
	var sqrSum float64
	for _, v := range arr {
		d := float64(v) - mean
		sqrSum += d * d
	}
	std = math.Sqrt(sqrSum / (n - 1))
	return mean, std
}

// Must panics in case of an error.
func Must(err error) {
	if err != nil {
		panic(err)
	}
}

// Mustf panics in case of an error for the given message format.
func Mustf(err error, format string, a ...any) {
	if err != nil {
		s := fmt.Sprintf(format, a...)
		panic(fmt.Errorf("%v: %w", s, err))
	}
}

// GetType is a convenient way to reparse a value to the required type.
// This solves the issue with unpredicted value types when unmarshalling a map.
func GetType[T any](m map[string]any, key string, defaultValue T) T {
	value, ok := m[key]
	if !ok {
		return defaultValue
	}

	typeVal, ok := value.(T)
	if ok {
		return typeVal
	}

	out, err := yaml.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("%s cannot be interpreted as %T: %w", key, typeVal, err))
	}

	if err = yaml.Unmarshal(out, &typeVal); err != nil {
		panic(fmt.Errorf("%s cannot be interpreted as %T: %w", key, typeVal, err))
	}

	return typeVal
}

// ReadFileIfExists returns the file's data in the given path.
// If the file does not exist, it returns nil.
func ReadFileIfExists(path string) []byte {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if os.IsNotExist(err) {
		return nil
	}
	Mustf(err, "failed reading file: %s", path)
	return data
}

// WriteFile writes the data to a file.
func WriteFile(path string, data []byte) {
	err := os.WriteFile(path, data, 0o644)
	Mustf(err, "failed writing file '%s'", path)
}

// Map maps an array to a new array of the same size using a transformation function.
func Map[T, K any](arr []T, mapper func(index int, value T) K) []K {
	ret := make([]K, len(arr))
	for i, v := range arr {
		ret[i] = mapper(i, v)
	}
	return ret
}
