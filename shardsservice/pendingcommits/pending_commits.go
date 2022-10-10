package pendingcommits

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("pending commits")

type PendingCommits interface {
	Add(tx TxID, sNumbers []token.SerialNumber)
	Get(tx TxID) []token.SerialNumber
	WaitTillNotExist(serialNumbers [][]byte)
	Delete(tx TxID)
	DeleteBatch(txIds []TxID)
	CountTxs() int
	CountSNs() int
	DeleteAll()
}

type TxID struct {
	BlkNum uint64
	TxNum  uint64
}
