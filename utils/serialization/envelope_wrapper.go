package serialization

import (
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

func MarshalTx(tx *token.Tx) []byte {
	data, err := proto.Marshal(tx)
	utils.Must(err)
	return data
}

func UnmarshalTx(data []byte) *token.Tx {
	var tx token.Tx
	utils.Must(proto.Unmarshal(data, &tx))
	return &tx
}

func WrapEnvelope(data []byte, header *common.Header) []byte {
	return protoutil.MarshalOrPanic(&common.Payload{
		Header: header,
		Data:   protoutil.MarshalOrPanic(&common.ConfigValue{Value: data}),
	})
}
func UnwrapEnvelope(message []byte) ([]byte, *common.ChannelHeader, error) {
	envelope, err := protoutil.GetEnvelopeFromBlock(message)
	if err != nil {
		return nil, nil, err
	}
	payload, err := protoutil.UnmarshalPayload(envelope.Payload)
	if err != nil {
		return nil, nil, err
	}
	channelHdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		return nil, nil, err
	}

	switch channelHdr.Type {
	case int32(common.HeaderType_MESSAGE):
		data := &common.ConfigValue{}
		if err = proto.Unmarshal(payload.Data, data); err != nil {
			return nil, nil, err
		} else {
			return data.Value, channelHdr, nil
		}
	default:
		// We are not interested in the data of a config tx
		return nil, channelHdr, nil
	}
}
