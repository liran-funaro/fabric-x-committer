package clients

import (
	"fmt"

	"github.com/golang/protobuf/proto"
	"github.com/hyperledger/fabric-protos-go/common"
	"github.com/hyperledger/fabric/protoutil"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/orderingservice/fabric/clients/pkg/identity"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
)

type envelopeCreator struct {
	txType     common.HeaderType
	channelID  string
	signer     identity.SignerSerializer
	msgVersion int32
	epoch      uint64
}

func newEnvelopeCreator(channelID string, signer identity.SignerSerializer) *envelopeCreator {
	return &envelopeCreator{txType: common.HeaderType_MESSAGE, channelID: channelID, signer: signer, msgVersion: 0, epoch: 0}
}
func (c *envelopeCreator) CreateSignedEnvelope(dataMsg proto.Message, tlsCertHash []byte) (*common.Envelope, error) {
	return c.createEnvelope(dataMsg, tlsCertHash, "", true)
}

// CreateEnvelope create an envelope WITHOUT a signature and the corresponding header
// can only be used with a patched fabric orderer
func (c *envelopeCreator) CreateEnvelope(dataMsg proto.Message, seqno uint64, tlsCertHash []byte) (*common.Envelope, error) {
	return c.createEnvelope(dataMsg, tlsCertHash, fmt.Sprintf("%d", seqno), false)
}

func (c *envelopeCreator) createEnvelope(dataMsg proto.Message, tlsCertHash []byte, txId string, signed bool) (*common.Envelope, error) {
	channelHeader := c.channelHeader(tlsCertHash, txId)

	data, err := proto.Marshal(dataMsg)
	if err != nil {
		return nil, errors.Wrap(err, "error marshaling")
	}
	payload := protoutil.MarshalOrPanic(&common.Payload{
		Header: protoutil.MakePayloadHeader(channelHeader, c.signatureHeader(signed)),
		Data:   data,
	})

	var sig []byte
	if signed && c.signer != nil {
		sig, err = c.signer.Sign(payload)
	}
	if err != nil {
		return nil, err
	}
	return &common.Envelope{Payload: payload, Signature: sig}, nil
}

func (c *envelopeCreator) signatureHeader(signed bool) *common.SignatureHeader {
	if !signed {
		header, err := NewSignatureHeader(c.signer)
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

func (c *envelopeCreator) channelHeader(tlsCertHash []byte, txId string) *common.ChannelHeader {
	h := protoutil.MakeChannelHeader(c.txType, c.msgVersion, c.channelID, c.epoch)
	h.TlsCertHash = tlsCertHash
	h.TxId = txId
	return h
}

func NewSignatureHeader(id identity.Serializer) (*common.SignatureHeader, error) {
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
