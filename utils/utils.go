package utils

import (
	"path/filepath"
	"runtime"
)

func Min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}

func CurrentDir() string {
	_, b, _, _ := runtime.Caller(1)
	return filepath.Dir(b)
}
