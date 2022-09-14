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

func NewBlockGenerator(numTxPerBlock, serialNumPerTx int) *BlockGenerator {
	g := &BlockGenerator{
		outputChan:     make(chan *token.Block, 2),
		stopSignalChan: make(chan struct{}),
	}
	g.startBlockGenRoutine(numTxPerBlock, serialNumPerTx)
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

func (g *BlockGenerator) startBlockGenRoutine(numTxPerBlock int, serialNumPerTx int) {
	nextBlockNum := uint64(0)
	go func() {
		for {
			select {
			case <-g.stopSignalChan:
				close(g.outputChan)
				return
			default:
				b := &token.Block{
					Number: nextBlockNum,
				}
				for i := 0; i < numTxPerBlock; i++ {
					serialNums := make([][]byte, serialNumPerTx)
					for j := 0; j < serialNumPerTx; j++ {
						sn := sha256.Sum256([]byte(fmt.Sprintf("%d", i*numTxPerBlock+j)))
						serialNums[j] = sn[:]
					}
					b.Txs = append(b.Txs,
						&token.Tx{
							SerialNumbers: serialNums,
						},
					)
				}
				g.outputChan <- b
			}
		}
	}()
}
