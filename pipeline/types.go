package pipeline

import "github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"

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

var statusMap = map[Status]string{
	UNKNOWN:           "Unknown",
	VALID:             "Valid",
	INVALID_SIGNATURE: "Invalid signature",
	DOUBLE_SPEND:      "Double spend",
}

func (s *Status) String() string {
	return statusMap[*s]
}
