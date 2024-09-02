package test

import (
	"testing"

	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
)

// CreateBlockForTest creates sample block with three txIDs.
func CreateBlockForTest(_ *testing.T, number uint64, preBlockHash []byte, txIDs [3]string) *common.Block {
	return &common.Block{
		Header: &common.BlockHeader{
			Number:       number,
			PreviousHash: preBlockHash,
		},
		Data: &common.BlockData{
			Data: [][]byte{
				createEnvelopeBytesForTest(nil, txIDs[0]),
				createEnvelopeBytesForTest(nil, txIDs[1]),
				createEnvelopeBytesForTest(nil, txIDs[2]),
			},
		},
	}
}

func createEnvelopeBytesForTest(_ *testing.T, txID string) []byte {
	header := &common.Header{
		ChannelHeader: protoutil.MarshalOrPanic(&common.ChannelHeader{
			ChannelId: "ch1",
			TxId:      txID,
			Type:      int32(common.HeaderType_MESSAGE),
		}),
	}
	return protoutil.MarshalOrPanic(&common.Envelope{
		Payload: protoutil.MarshalOrPanic(&common.Payload{
			Header: header,
			Data:   protoutil.MarshalOrPanic(&protoblocktx.Tx{Id: txID}),
		},
		),
	},
	)
}
