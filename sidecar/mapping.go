package sidecar

import (
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
	"google.golang.org/protobuf/proto"
)

type validationCode = byte

const (
	excludedStatus  = validationCode(peer.TxValidationCode_VALID)
	notYetValidated = validationCode(peer.TxValidationCode_NOT_VALIDATED)
)

func newValidationCodes(expected int) []validationCode {
	codes := make([]validationCode, expected)
	for i := range codes {
		codes[i] = notYetValidated
	}
	return codes
}

func mapBlock(block *common.Block) (*protoblocktx.Block, []int) {
	excluded := make([]int, 0, len(block.Data.Data))
	txs := make([]*protoblocktx.Tx, 0, len(block.Data.Data))
	txsNum := make([]uint32, 0, len(block.Data.Data))
	for txNum, msg := range block.Data.Data {
		logger.Debugf("Mapping transaction [blk,tx] = [%d,%d]", block.Header.Number, txNum)
		if data, channelHdr, err := serialization.UnwrapEnvelope(msg); err != nil {
			logger.Fatalf("error unwrapping envelope: %v", err)
		} else if channelHdr.Type != int32(common.HeaderType_MESSAGE) {
			logger.Debugf("Ignoring TX [%s] of type %d", channelHdr.TxId, channelHdr.Type)
			excluded = append(excluded, txNum)
		} else if tx, err := UnmarshalTx(data); err != nil {
			logger.Fatalf("error unmarshaling MESSAGE tx [%s] tx: %v", channelHdr.TxId, err)
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

// UnmarshalTx unmarshals data bytes to protoblocktx.Tx.
func UnmarshalTx(data []byte) (*protoblocktx.Tx, error) {
	var tx protoblocktx.Tx
	if err := proto.Unmarshal(data, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}
