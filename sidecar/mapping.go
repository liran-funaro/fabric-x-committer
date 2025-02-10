package sidecar

import (
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"google.golang.org/protobuf/proto"
)

type validationCode = byte

const (
	excludedStatus  = validationCode(protoblocktx.Status_ABORTED_UNSUPPORTED_TX_PAYLOAD)
	notYetValidated = validationCode(protoblocktx.Status_NOT_VALIDATED)
)

func newValidationCodes(expected int) []validationCode {
	codes := make([]validationCode, expected)
	for i := range codes {
		codes[i] = notYetValidated
	}
	return codes
}

func mapBlock(block *common.Block) (*protoblocktx.Block, []int) {
	if block.Data == nil {
		logger.Warnf("Received a block [%d] without data", block.Header.Number)
		return &protoblocktx.Block{
			Number: block.Header.Number,
		}, nil
	}

	excluded := make([]int, 0, len(block.Data.Data))
	txs := make([]*protoblocktx.Tx, 0, len(block.Data.Data))
	txsNum := make([]uint32, 0, len(block.Data.Data))
	for txNum, msg := range block.Data.Data {
		logger.Debugf("Mapping transaction [blk,tx] = [%d,%d]", block.Header.Number, txNum)
		if data, channelHdr, err := serialization.UnwrapEnvelope(msg); err != nil {
			excluded = exclude(excluded, txNum, channelHdr, err.Error())
		} else if channelHdr.Type != int32(common.HeaderType_MESSAGE) {
			excluded = exclude(excluded, txNum, channelHdr, "unsupported type")
		} else if tx, err := UnmarshalTx(data); err != nil {
			excluded = exclude(excluded, txNum, channelHdr, err.Error())
		} else {
			logger.Debugf("Appended txID [%s] -> [%s]", channelHdr.TxId, tx.Id)
			txs = append(txs, tx)
			txsNum = append(txsNum, uint32(txNum)) //nolint:gosec
		}
	}
	return &protoblocktx.Block{
		Number: block.Header.Number,
		Txs:    txs,
		TxsNum: txsNum,
	}, excluded
}

func exclude(excluded []int, txNum int, channelHdr *common.ChannelHeader, reason string) []int {
	logger.Debugf("Excluding TX [%s] of type %d due to %s", channelHdr.TxId, channelHdr.Type, reason)
	return append(excluded, txNum)
}

// UnmarshalTx unmarshals data bytes to protoblocktx.Tx.
func UnmarshalTx(data []byte) (*protoblocktx.Tx, error) {
	var tx protoblocktx.Tx
	if err := proto.Unmarshal(data, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}
