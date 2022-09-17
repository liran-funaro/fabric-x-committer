package testutil

import (
	"crypto/sha256"
	"fmt"

	"github.ibm.com/distributed-trust-research/scalable-committer/token"
)

type BlockGenerator struct {
	outputChan     chan *token.Block
	stopSignalChan chan struct{}
}

func NewBlockGenerator(numTxPerBlock, serialNumPerTx int, addSignatureBytes bool) *BlockGenerator {
	g := &BlockGenerator{
		outputChan:     make(chan *token.Block, 2),
		stopSignalChan: make(chan struct{}),
	}
	g.startBlockGenRoutine(numTxPerBlock, serialNumPerTx, addSignatureBytes)
	return g
}

func (g *BlockGenerator) OutputChan() <-chan *token.Block {
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
		randomBytesForSignature = make([]byte, 1000) // 1kb
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
				b := &token.Block{
					Number: blockNum,
				}
				for i := 0; i < numTxPerBlock; i++ {
					serialNums := make([][]byte, serialNumPerTx)
					for j := 0; j < serialNumPerTx; j++ {
						sn := sha256.Sum256([]byte(fmt.Sprintf("%d", uniqueSerialNum)))
						uniqueSerialNum++
						serialNums[j] = sn[:]
					}
					b.Txs = append(b.Txs,
						&token.Tx{
							SerialNumbers: serialNums,
							Signature:     randomBytesForSignature,
						},
					)
				}
				g.outputChan <- b
				blockNum++
			}
		}
	}()
}
