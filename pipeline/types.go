package pipeline

import "fmt"

const defaultChannelBufferSize = 100

type TxSeqNum struct {
	BlkNum, TxNum uint64
}

type TxStatus struct {
	TxSeqNum TxSeqNum
	IsValid  bool
}

func (n TxSeqNum) String() string {
	return fmt.Sprintf("txSeq:%d:%d", n.BlkNum, n.TxNum)
}
