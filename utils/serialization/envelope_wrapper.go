package serialization

import (
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/hyperledger/fabric/protoutil"
)

func WrapEnvelope(data []byte, header *common.Header) []byte {
	return protoutil.MarshalOrPanic(&common.Payload{
		Header: header,
		Data:   data,
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
		return payload.Data, channelHdr, nil
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
