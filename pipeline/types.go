package pipeline

import "fmt"

type blkNumTxNum struct {
	blkNum, txNum uint64
}

type txValidationStatus struct {
	blkNumTxNum blkNumTxNum
	isValid     bool
}

func (b blkNumTxNum) String() string {
	return fmt.Sprintf("Block:%d, Tx:%d", b.blkNum, b.txNum)
}
