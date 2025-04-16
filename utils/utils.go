package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cockroachdb/errors"
	"golang.org/x/exp/constraints"
)

// ErrActiveStream represents the error when attempting to create a new stream while one is already active.
// The system only allows a single active stream at any given time.
var ErrActiveStream = errors.New("a stream is already active. Only one active stream is allowed at a time")

// FileExists returns true if a file path exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// LazyJSON will lazily marshal a struct for logging purposes.
type LazyJSON struct {
	O any
}

// String marshals the give object as JSON.
func (lj *LazyJSON) String() string {
	if p, err := json.Marshal(lj.O); err != nil {
		return fmt.Sprintf("cannot marshal object: %v", err)
	} else {
		return string(p)
	}
}

// Must panics given an error.
func Must(err error, msg ...string) {
	if err != nil {
		panic(errors.Wrap(err, strings.Join(msg, " ")))
	}
}

// FirstSuccessful returns the first successful call to method given the available operands.
func FirstSuccessful[A, T any](operands []A, method func(A) (T, error)) (T, error) {
	var errs []error //nolint:prealloc // error is unlikely.
	for _, op := range operands {
		retValue, err := method(op)
		if err == nil {
			return retValue, nil
		}
		errs = append(errs, err)
	}
	return *new(T), errors.Join(errs...)
}

// Range returns a slice containing integers in the range from start (including) to end (excluding).
func Range[T constraints.Integer](start, end T) []T {
	if start >= end {
		return nil
	}
	results := make([]T, 0, end-start)
	for i := start; i < end; i++ {
		results = append(results, i)
	}
	return results
}
