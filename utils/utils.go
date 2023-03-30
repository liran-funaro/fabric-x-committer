package utils

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/pkg/errors"
)

func Every(interval time.Duration, runnable func()) {
	go func() {
		for {
			<-time.After(interval)
			runnable()
		}
	}()
}

func Min(a int, b int) int {
	min, _ := sorted(a, b)
	return min
}

func Max(a, b int) int {
	_, max := sorted(a, b)
	return max
}

func sorted(a, b int) (int, int) {
	if a < b {
		return a, b
	}
	return b, a
}

func CurrentDir() string {
	_, b, _, _ := runtime.Caller(1)
	dir := filepath.Dir(b)
	if !FileExists(dir) {
		return "."
	}
	return dir
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func OverwriteFile(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open %s", path)
	}
	return file, nil
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

func ReadFile(filePath string) ([]byte, error) {
	absoluteFilePath, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(absoluteFilePath)
	if err != nil {
		return nil, err
	}

	return ioutil.ReadAll(file)
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
