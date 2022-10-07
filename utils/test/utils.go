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

var TxSize = 1
var ClientInputDelay = NoDelay
var BatchSize = 100
