package token

import (
	"fmt"
)

type SerialNumber = []byte
type TxOutput = []byte

type TxSeqNum struct {
	BlkNum, TxNum uint64
}

func (n TxSeqNum) String() string {
	return fmt.Sprintf("txSeq:%d:%d", n.BlkNum, n.TxNum)
}

func TxSeqNumFromString(s string) *TxSeqNum {
	return &TxSeqNum{
		BlkNum: 0,
		TxNum:  0,
	}

	// sp := strings.Split(s, ":")
	// if len(sp) != 3 {
	// 	panic("string not compatible with TxSeqNum")
	// }

	// blkNum, err := strconv.ParseUint(sp[1], 0, 64)
	// if err != nil {
	// 	panic(err)
	// }

	// txNum, err := strconv.ParseUint(sp[2], 0, 64)
	// if err != nil {
	// 	panic(err)
	// }

	// return &TxSeqNum{BlkNum: blkNum, TxNum: txNum}
}
