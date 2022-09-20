package utils

import (
	"os"
	"path/filepath"
	"runtime"

	"github.com/pkg/errors"
)

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

func Must(err error) {
	if err != nil {
		panic(err)
	}
}
