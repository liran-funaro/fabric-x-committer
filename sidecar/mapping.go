package sidecar

import (
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric-protos-go/peer"
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
	for i, msg := range block.Data.Data {
		logger.Debugf("Mapping transaction [blk,tx] = [%d,%d]", block.Header.Number, i)
		if data, channelHdr, err := serialization.UnwrapEnvelope(msg); err != nil {
			logger.Fatalf("error unwrapping envelope: %v", err)
		} else if channelHdr.Type != int32(common.HeaderType_MESSAGE) {
			logger.Debugf("Ignoring TX [%s] of type %d", channelHdr.TxId, channelHdr.Type)
			excluded = append(excluded, i)
		} else if tx, err := UnmarshalTx(data); err != nil {
			logger.Fatalf("error unmarshaling MESSAGE tx [%s] tx: %v", channelHdr.TxId, err)
		} else {
			logger.Debugf("Appended txID [%s] -> [%s]", channelHdr.TxId, tx.Id)
			txs = append(txs, tx)
		}
	}
	return &protoblocktx.Block{
		Number: block.Header.Number,
		Txs:    txs,
	}, excluded
}

func UnmarshalTx(data []byte) (*protoblocktx.Tx, error) {
	var tx protoblocktx.Tx
	if err := proto.Unmarshal(data, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}
