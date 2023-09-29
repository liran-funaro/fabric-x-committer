package workload

import (
	"testing"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

var result *BlockWithExpectedResult
var resultBlock *protoblocktx.Block

// go test -bench . -benchmem -memprofile -blockprofile -cpuprofile profile.out
// go tool pprof profile.out

func BenchmarkAAA(b *testing.B) {
	var r *BlockWithExpectedResult
	for n := 0; n < b.N; n++ {
		_, bQueue, _ := GetBlockWorkload("../../wgclient/out/blocks")
		for block := range bQueue {
			r = block
		}
	}
	result = r
}

func BenchmarkBBB(b *testing.B) {
	var r *protoblocktx.Block
	for n := 0; n < b.N; n++ {
		bg := NewBlockGenerator(100, 1, true)
		for i := 0; i < 100000; i++ {
			r = <-bg.OutputChan()
		}
	}
	resultBlock = r
}

var r *protoblocktx.Tx

func BenchmarkGenSingle(b *testing.B) {

	sigType := "ECDSA"
	privateKey, _ := sigverification_test.GetSignatureFactory(sigType).NewKeys()
	signer, _ := sigverification_test.GetSignatureFactory(sigType).NewSigner(privateKey)

	vr := 1.0

	g := NewConflictDecorator(&sigverification_test.ValidTxGenerator{
		TxSigner:                signer,
		TxSerialNumberGenerator: sigverification_test.NewLinearTxInputGenerator([]test.DiscreteValue{{1, 1}}),
		TxOutputGenerator:       sigverification_test.NewLinearTxInputGenerator([]test.DiscreteValue{{1, 1}}),
	}, &ConflictProfile{Statistical: &StatisticalConflicts{InvalidSignatures: vr}}, signer.SignTx)

	var tx *protoblocktx.Tx
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		tx = g.Next().Tx
	}
	r = tx
}
