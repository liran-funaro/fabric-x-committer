package pipeline

import "fmt"

const defaultChannelBufferSize = 1000

type txSeqNum struct {
	blkNum, txNum uint64
}

type txStatus struct {
	txSeqNum txSeqNum
	isValid  bool
}

func (n txSeqNum) String() string {
	return fmt.Sprintf("txSeq:%d:%d", n.blkNum, n.txNum)
}
