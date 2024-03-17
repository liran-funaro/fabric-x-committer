package workload

import (
	"crypto/sha256"
	"fmt"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

type BlockGenerator struct {
	outputChan     chan *protoblocktx.Block
	stopSignalChan chan struct{}
}

func NewBlockGenerator(numTxPerBlock, serialNumPerTx int, addSignatureBytes bool) *BlockGenerator {
	g := &BlockGenerator{
		outputChan:     make(chan *protoblocktx.Block, 100),
		stopSignalChan: make(chan struct{}),
	}
	g.startBlockGenRoutine(numTxPerBlock, serialNumPerTx, addSignatureBytes)
	return g
}

func (g *BlockGenerator) OutputChan() <-chan *protoblocktx.Block {
	return g.outputChan
}

func (g *BlockGenerator) Stop() {
	go func() {
		for range g.outputChan {
		}
	}()
	close(g.stopSignalChan)
}

func (g *BlockGenerator) startBlockGenRoutine(numTxPerBlock, serialNumPerTx int, addSignatureBytes bool) {
	randomBytesForSignature := []byte{}
	if addSignatureBytes {
		randomBytesForSignature = make([]byte, 72) // DER encoded ECDSA signature
		for i := 0; i < len(randomBytesForSignature); i++ {
			randomBytesForSignature[i] = uint8(i)
		}
	}

	blockNum := uint64(0)
	uniqueSerialNum := 0

	go func() {
		for {
			select {
			case <-g.stopSignalChan:
				close(g.outputChan)
				return
			default:
				b := &protoblocktx.Block{
					Number: blockNum,
				}
				for i := 0; i < numTxPerBlock; i++ {
					serialNums := make([][]byte, serialNumPerTx)
					for j := 0; j < serialNumPerTx; j++ {
						sn := sha256.Sum256([]byte(fmt.Sprintf("%d", uniqueSerialNum)))
						uniqueSerialNum++
						serialNums[j] = sn[:]
					}
					tx := &protoblocktx.Tx{
						Namespaces: []*protoblocktx.TxNamespace{{
							NsId:        1,
							BlindWrites: []*protoblocktx.Write{},
						}},
						Signatures: [][]byte{randomBytesForSignature},
					}

					for _, sn := range serialNums {
						tx.Namespaces[0].BlindWrites = append(tx.Namespaces[0].BlindWrites, &protoblocktx.Write{Key: sn})
					}

					b.Txs = append(b.Txs, tx)
				}
				g.outputChan <- b
				blockNum++
			}
		}
	}()
}
