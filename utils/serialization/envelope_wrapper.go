package serialization

import (
	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
)

func MarshalTx(tx *protoblocktx.Tx) []byte {
	return protoutil.MarshalOrPanic(tx)
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

	payload, channelHdr, err := ParseEnvelope(envelope)
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
	case int32(common.HeaderType_ENDORSER_TRANSACTION):
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

func ParseEnvelope(envelope *common.Envelope) (*common.Payload, *common.ChannelHeader, error) {
	payload, err := protoutil.UnmarshalPayload(envelope.Payload)
	if err != nil {
		return nil, nil, err
	}
	channelHdr, err := protoutil.UnmarshalChannelHeader(payload.Header.ChannelHeader)
	if err != nil {
		return nil, nil, err
	}
	return payload, channelHdr, nil
}

func IsConfigTx(envBytes []byte) bool {
	envelope, err := protoutil.GetEnvelopeFromBlock(envBytes)
	if err != nil {
		return false
	}

	_, channelHdr, err := ParseEnvelope(envelope)
	if err != nil {
		return false
	}

	return common.HeaderType(channelHdr.Type) == common.HeaderType_CONFIG
}
