package workload

import (
	"testing"

	"github.ibm.com/distributed-trust-research/scalable-committer/pipeline/testutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

var result *token.Block

// go test -bench . -benchmem -memprofile -blockprofile -cpuprofile profile.out
// go tool pprof profile.out

func BenchmarkAAA(b *testing.B) {
	var r *token.Block
	for n := 0; n < b.N; n++ {
		_, bQueue, _ := GetBlockWorkload("../../wgclient/out/blocks")
		for block := range bQueue {
			r = block
		}
	}
	result = r
}

func BenchmarkBBB(b *testing.B) {
	var r *token.Block
	for n := 0; n < b.N; n++ {
		bg := testutil.NewBlockGenerator(100, 1, true)
		for i := 0; i < 100000; i++ {
			r = <-bg.OutputChan()
		}
	}
	result = r
}
