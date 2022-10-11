package pipeline

import "github.ibm.com/distributed-trust-research/scalable-committer/token"

const defaultChannelBufferSize = 100

type TxSeqNum = token.TxSeqNum

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
