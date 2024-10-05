package utils

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/pkg/errors"
)

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func OverwriteFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open %s", path)
	}
	return file, nil
}

// LazyJson will lazily marshal a struct for logging purposes
func LazyJson(v any) *lazyJson {
	return &lazyJson{v: v}
}

type lazyJson struct {
	v any
}

func (d *lazyJson) String() string {
	if p, err := json.Marshal(d.v); err != nil {
		return fmt.Sprintf("cannot marshal object: %v", err)
	} else {
		return string(p)
	}
}

func WriteFile(path string, data []byte) error {
	file, err := OverwriteFile(path)
	defer file.Close()
	if err != nil {
		return err
	}
	_, err = file.Write(data)
	return err
}

func Must(err error, msg ...string) {
	if err != nil {
		panic(errors.Wrapf(err, "%v", msg))
	}
}

func Require(condition bool, msg string) {
	if !condition {
		panic(errors.New(msg))
	}
}

func ReadFile(filePath string) ([]byte, error) {
	absoluteFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absoluteFilePath)
	if err != nil {
		return nil, err
	}
	defer func(file *os.File) {
		_ = file.Close()
	}(file)
	return io.ReadAll(file)
}

func UniformBuckets(count int, from, to float64) []float64 {
	if to < from {
		panic("invalid input")
	}
	result := make([]float64, 0, count)
	step := (to - from) / float64(count-1)
	for low := from; low < to; low += step {
		result = append(result, low)
	}
	return append(result, to)
}

type ConsensusType = string

const (
	Raft ConsensusType = "etcdraft"
	Bft                = "BFT"
)

// Retry executes the given operation repeatedly until it succeeds or a timeout occurs.
// It returns nil on success, or the error returned by the final attempt on timeout.
func Retry(o backoff.Operation, timeout time.Duration) error {
	if timeout.Seconds() == 0 {
		return errors.New("Invalid timeout value. The timeout must be a positive number to prevent infinite retries.")
	}
	expBackoff := backoff.NewExponentialBackOff()
	expBackoff.MaxElapsedTime = timeout
	return backoff.Retry(o, expBackoff)
}

// ErrActiveStream represents the error when attempting to create a new stream while one is already active.
// The system only allows a single active stream at any given time.
var ErrActiveStream = errors.New("a stream is already active. Only one active stream is allowed at a time")
