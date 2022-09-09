package utils

import (
	"path/filepath"
	"runtime"
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
	return filepath.Dir(b)
}

func Must(err error) {
	if err != nil {
		panic(err)
	}
}
