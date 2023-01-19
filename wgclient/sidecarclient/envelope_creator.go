package sidecarclient

import (
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/identity"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/serialization"
)

//EnvelopeCreator takes serialized data in its input, e.g. a marshaled TX and creates an envelope to send to the orderer.
type EnvelopeCreator interface {
	CreateEnvelope(data []byte) (*common.Envelope, error)
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
func (c *envelopeCreator) CreateEnvelope(data []byte) (*common.Envelope, error) {
	payload := serialization.WrapEnvelope(data, c.payloadHeader())

	signature, err := c.sign(payload)
	if err != nil {
		return nil, err
	}
	return &common.Envelope{Payload: payload, Signature: signature}, nil
}

func (c *envelopeCreator) sign(payload []byte) ([]byte, error) {
	if !c.signed {
		return []byte{}, nil
	}
	return c.signer.Sign(payload)
}

func (c *envelopeCreator) payloadHeader() *common.Header {
	signatureHeader := protoutil.NewSignatureHeaderOrPanic(c.signer)

	channelHeader := protoutil.MakeChannelHeader(c.txType, c.msgVersion, c.channelID, c.epoch)
	channelHeader.TlsCertHash = c.tlsCertHash
	channelHeader.TxId = protoutil.ComputeTxID(signatureHeader.Nonce, signatureHeader.Creator)

	return protoutil.MakePayloadHeader(channelHeader, signatureHeader)
}
