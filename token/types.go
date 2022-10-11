package token

import "fmt"

type SerialNumber = []byte

type TxSeqNum struct {
	BlkNum, TxNum uint64
}

func (n TxSeqNum) String() string {
	return fmt.Sprintf("txSeq:%d:%d", n.BlkNum, n.TxNum)
}
