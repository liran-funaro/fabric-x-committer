package test

import (
	"testing"

	"github.com/onsi/gomega"
)

func FailHandler(t *testing.T) {
	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		t.Fatalf(message)
	})
}
