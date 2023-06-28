package pendingcommits

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

var logger = logging.New("pending commits")

type PendingCommits interface {
	Add(tx TxID, sNumbers []token.SerialNumber)
	Get(tx TxID) []token.SerialNumber
	WaitTillNotExist(serialNumbers []token.SerialNumber)
	Delete(tx TxID)
	DeleteBatch(txIds []TxID)
	CountTxs() int
	CountSNs() int
	DeleteAll()
}

type TxID = token.TxSeqNum
