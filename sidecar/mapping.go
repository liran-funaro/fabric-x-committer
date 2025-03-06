package sidecar

import (
	"fmt"

	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"go.uber.org/zap/zapcore"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type (
	validationCode    = byte
	scBlockWithStatus struct {
		block      *protoblocktx.Block
		withStatus *blockWithStatus
		isConfig   bool
	}
)

const (
	excludedStatus  = validationCode(protoblocktx.Status_ABORTED_UNSUPPORTED_TX_PAYLOAD)
	notYetValidated = validationCode(protoblocktx.Status_NOT_VALIDATED)
)

func mapBlock(block *common.Block) *scBlockWithStatus {
	if block.Data == nil {
		logger.Warnf("Received a block [%d] without data", block.Header.Number)
		return &scBlockWithStatus{
			block: &protoblocktx.Block{
				Number: block.Header.Number,
			},
			withStatus: &blockWithStatus{
				block:         block,
				txIDToTxIndex: make(map[string]int),
			},
		}
	}
	txCount := len(block.Data.Data)
	mappedBlock := &scBlockWithStatus{
		block: &protoblocktx.Block{
			Number: block.Header.Number,
			Txs:    make([]*protoblocktx.Tx, 0, txCount),
			TxsNum: make([]uint32, 0, txCount),
		},
		withStatus: &blockWithStatus{
			block:         block,
			txStatus:      newValidationCodes(txCount),
			txIDToTxIndex: make(map[string]int, txCount),
			pendingCount:  txCount,
		},
	}

	for txNum, msg := range block.Data.Data {
		logger.Debugf("Mapping transaction [blk,tx] = [%d,%d]", block.Header.Number, txNum)
		data, hdr, err := serialization.UnwrapEnvelope(msg)
		if err != nil {
			mappedBlock.excludeTx(txNum, hdr, err.Error())
			continue
		}

		switch hdr.Type {
		case int32(common.HeaderType_CONFIG):
			mappedBlock.appendTx(txNum, hdr, configTx(hdr.TxId, msg))
			mappedBlock.isConfig = true
		case int32(common.HeaderType_MESSAGE):
			tx, err := serialization.UnmarshalTx(data)
			if err != nil {
				mappedBlock.excludeTx(txNum, hdr, err.Error())
				continue
			}
			if !preValidateTx(tx) {
				mappedBlock.excludeTx(txNum, hdr, "pre-validation error")
				continue
			}
			mappedBlock.appendTx(txNum, hdr, tx)
		default:
			mappedBlock.excludeTx(txNum, hdr, "unsupported type")
		}
	}
	return mappedBlock
}

func (b *scBlockWithStatus) appendTx(txNum int, channelHdr *common.ChannelHeader, tx *protoblocktx.Tx) {
	b.block.TxsNum = append(b.block.TxsNum, uint32(txNum)) //nolint:gosec
	b.block.Txs = append(b.block.Txs, tx)
	debugTx(channelHdr, "including [==> %s]", tx.Id)
}

func (b *scBlockWithStatus) excludeTx(txNum int, channelHdr *common.ChannelHeader, reason string) {
	b.withStatus.txStatus[txNum] = excludedStatus
	b.withStatus.pendingCount--
	debugTx(channelHdr, "excluding due to %s", reason)
}

func debugTx(channelHdr *common.ChannelHeader, format string, a ...any) {
	if logger.Level() > zapcore.DebugLevel {
		return
	}
	hdr := "<no-header>"
	txID := "<no-id>"
	if channelHdr != nil {
		hdr = common.HeaderType(channelHdr.Type).String()
		txID = channelHdr.TxId
	}
	logger.Debugf("TX type [%s] ID [%s]: %s", hdr, txID, fmt.Sprintf(format, a...))
}

func newValidationCodes(expected int) []validationCode {
	codes := make([]validationCode, expected)
	for i := range codes {
		codes[i] = notYetValidated
	}
	return codes
}

func configTx(id string, value []byte) *protoblocktx.Tx {
	return &protoblocktx.Tx{
		Id: id,
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      types.MetaNamespaceID,
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key:   []byte(types.MetaNamespaceID),
				Value: value,
			}},
		}},
		// This flags the verifier that this is a valid config TX.
		// Any other TX that will have this signature format, will be filtered by the sidecar.
		Signatures: make([][]byte, 1),
	}
}

// preValidateTx the sidecar does not sign the config TX, and thus the policy verifier only verify that a
// config TX has a single empty signature.
// To ensure clients cannot abuse this to send such unsigned transaction, we pre-validate that the transaction
// does not have a single empty signature.
// This way, even if the client sends a config TX, it will be ignored.
func preValidateTx(tx *protoblocktx.Tx) bool {
	signatures := tx.GetSignatures()
	return len(signatures) != 1 || len(signatures[0]) > 0
}
