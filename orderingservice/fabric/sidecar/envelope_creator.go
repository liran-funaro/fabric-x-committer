package sidecar

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/pkg/identity"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

//EnvelopeCreator takes serialized data in its input, e.g. a marshaled TX and creates an envelope to send to the orderer.
type EnvelopeCreator interface {
	CreateEnvelope(data []byte, seqNo uint64) (*common.Envelope, error)
}

type envelopeCreator struct {
	txType      common.HeaderType
	channelID   string
	signer      identity.SignerSerializer
	msgVersion  int32
	epoch       uint64
	tlsCertHash []byte
	signed      bool
}

func newEnvelopeCreator(channelID string, signer identity.SignerSerializer, signed bool) *envelopeCreator {
	return &envelopeCreator{
		txType:      common.HeaderType_MESSAGE,
		channelID:   channelID,
		signer:      signer,
		msgVersion:  0,
		epoch:       0,
		tlsCertHash: nil,
		signed:      signed,
	}
}

// CreateEnvelope create an envelope with or without a signature and the corresponding header
// An unsigned envelope can only be used with a patched fabric orderer
func (c *envelopeCreator) CreateEnvelope(data []byte, seqNo uint64) (*common.Envelope, error) {
	channelHeader := c.channelHeader(seqNo)

	data, err := proto.Marshal(&common.ConfigValue{Value: data})
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling")
	}
	payload := protoutil.MarshalOrPanic(&common.Payload{
		Header: protoutil.MakePayloadHeader(channelHeader, c.signatureHeader()),
		Data:   data,
	})

	var sig []byte
	if c.signed && c.signer != nil {
		sig, err = c.signer.Sign(payload)
	}
	if err != nil {
		return nil, err
	}
	return &common.Envelope{Payload: payload, Signature: sig}, nil
}

func (c *envelopeCreator) signatureHeader() *common.SignatureHeader {
	if !c.signed {
		header, err := newSignatureHeader(c.signer)
		utils.Must(err)
		return header
	}
	if c.signer == nil {
		return &common.SignatureHeader{}
	}
	// TODO create a "lightweight" header
	payloadSignatureHeader, err := protoutil.NewSignatureHeader(c.signer)
	utils.Must(err)
	return payloadSignatureHeader
}

func (c *envelopeCreator) channelHeader(seqNo uint64) *common.ChannelHeader {
	h := protoutil.MakeChannelHeader(c.txType, c.msgVersion, c.channelID, c.epoch)
	h.TlsCertHash = c.tlsCertHash
	if !c.signed {
		h.TxId = fmt.Sprintf("%d", seqNo)
	}
	return h
}

func newSignatureHeader(id identity.Serializer) (*common.SignatureHeader, error) {
	//creator, err := id.Serialize()
	//if err != nil {
	//	return nil, err
	//}

	creator := []byte("bob")

	nonce, err := protoutil.CreateNonce()
	if err != nil {
		return nil, err
	}

	return &common.SignatureHeader{
		Creator: creator,
		Nonce:   nonce,
	}, nil
}
