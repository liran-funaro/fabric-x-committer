package workload

import (
	"testing"

	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"

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

var r *token.Tx

func BenchmarkGenSingle(b *testing.B) {

	sigType := "ECDSA"
	privateKey, _ := sigverification_test.GetSignatureFactory(sigType).NewKeys()
	signer, _ := sigverification_test.GetSignatureFactory(sigType).NewSigner(privateKey)

	snCount := int64(1)
	vr := 1.0

	g := &sigverification_test.TxGenerator{
		TxSigner:               signer,
		TxInputGenerator:       &sigverification_test.LinearTxInputGenerator{Count: snCount},
		ValidSigRatioGenerator: test.NewBooleanGenerator(test.PercentageUniformDistribution, test.Percentage(vr), 10),
	}

	var tx *token.Tx
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		tx = g.Next()
	}
	r = tx
}
