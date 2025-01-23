package broadcastdeliver

import (
	"github.com/hyperledger/fabric-protos-go-apiv2/common"
	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/hyperledger/fabric/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/serialization"
)

type (
	// EnvelopeCreator takes serialized data in its input,
	// e.g. a marshaled TX and creates an envelope to send to the orderer.
	EnvelopeCreator struct {
		txType     common.HeaderType
		channelID  string
		signer     protoutil.Signer
		msgVersion int32
		epoch      uint64
	}

	// EnvelopedStream implements broadcast, with the addition of automatically signing the envelope.
	EnvelopedStream struct {
		BroadcastStream
		envelopeCreator *EnvelopeCreator
	}

	// BroadcastStream can be either CFT or BFT broadcast.
	BroadcastStream interface {
		Submit(*common.Envelope) (*ab.BroadcastResponse, error)
	}

	// EnvelopeTxID the TX ID generated for the envelope.
	EnvelopeTxID = string

	noOpSigner struct{}
)

// NewEnvelopeCreator instantiate an envelope creator.
func NewEnvelopeCreator(channelID string, signer protoutil.Signer) *EnvelopeCreator {
	logger.Infof("Creating envelope creator for channel %s", channelID)

	if signer == nil {
		logger.Infof("No-op signer used for envelope creator for channel %s.", channelID)
		signer = &noOpSigner{}
	}

	return &EnvelopeCreator{
		// TODO: If we set it to ENDORSER_MESSAGE, the FSC nodes crash when they try to read the TXs.
		txType:     common.HeaderType_MESSAGE,
		channelID:  channelID,
		signer:     signer,
		msgVersion: 0,
		epoch:      0,
	}
}

// CreateEnvelope create an envelope with or without a signature and the corresponding header.
// An unsigned envelope can only be used with a patched fabric orderer.
func (c *EnvelopeCreator) CreateEnvelope(data []byte) (*common.Envelope, EnvelopeTxID, error) {
	header, txID := c.payloadHeader()
	payload := serialization.WrapEnvelope(data, header)

	signature, err := c.signer.Sign(payload)
	if err != nil {
		return nil, "", err
	}
	return &common.Envelope{Payload: payload, Signature: signature}, txID, nil
}

func (c *EnvelopeCreator) payloadHeader() (*common.Header, EnvelopeTxID) {
	signatureHeader := protoutil.NewSignatureHeaderOrPanic(c.signer)
	channelHeader := protoutil.MakeChannelHeader(c.txType, c.msgVersion, c.channelID, c.epoch)
	channelHeader.TxId = protoutil.ComputeTxID(signatureHeader.Nonce, signatureHeader.Creator)
	return protoutil.MakePayloadHeader(channelHeader, signatureHeader), channelHeader.TxId
}

// SubmitWithEnv wraps a payload with an envelope, signs it and submit it to the stream.
func (s *EnvelopedStream) SubmitWithEnv(b []byte) (EnvelopeTxID, *ab.BroadcastResponse, error) {
	env, txID, err := s.envelopeCreator.CreateEnvelope(b)
	if err != nil {
		return txID, nil, err
	}
	resp, err := s.Submit(env)
	return txID, resp, err
}

func (*noOpSigner) Serialize() ([]byte, error)  { return []byte{}, nil }
func (*noOpSigner) Sign([]byte) ([]byte, error) { return nil, nil }
