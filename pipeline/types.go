package pipeline

import "fmt"

const defaultChannelBufferSize = 100

type TxSeqNum struct {
	BlkNum, TxNum uint64
}

type TxStatus struct {
	TxSeqNum TxSeqNum
	Status   Status
}

type Status int

const (
	UNKNOWN Status = iota
	VALID
	INVALID_SIGNATURE
	DOUBLE_SPEND
)

func (n TxSeqNum) String() string {
	return fmt.Sprintf("txSeq:%d:%d", n.BlkNum, n.TxNum)
}
