package workload

import (
	sigverification_test "github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
	"google.golang.org/protobuf/proto"
)

func GetBlockSize(pp *Profile, sampleSize int) float64 {
	privateKey, _, _ := sigverification_test.ReadOrGenerateKeys(pp.Transaction.Signature)
	signer, _ := sigverification_test.GetSignatureFactory(pp.Transaction.Signature.Scheme).NewSigner(privateKey)

	g := &sigverification_test.TxGenerator{
		TxSigner:                signer,
		TxSerialNumberGenerator: sigverification_test.NewLinearTxInputGenerator(pp.Transaction.SerialNumberSize),
		TxOutputGenerator:       sigverification_test.NewLinearTxInputGenerator(pp.Transaction.OutputSize),
		ValidSigRatioGenerator:  test.NewBooleanGenerator(test.PercentageUniformDistribution, test.Always, 10),
	}

	sum := 0
	for i := 0; i < sampleSize; i++ {
		txs := make([]*token.Tx, pp.Block.Size)
		for i := int64(0); i < pp.Block.Size; i++ {
			txs[i], _ = g.Next()
		}

		b := token.Block{
			Number: 0,
			Txs:    txs,
		}
		sum += calculateBlockSize(b)
	}
	return float64(sum) / float64(sampleSize)
}

func calculateBlockSize(b token.Block) int {
	data, err := proto.Marshal(&b)
	if err != nil {
		panic(err)
	}
	return len(data)
}
