package verifierserver_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/distributed-trust-research/scalable-committer/token"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/test"
)

const testTimeout = 3 * time.Second

var parallelExecutionConfig = &parallelexecutor.Config{
	BatchSizeCutoff:   3,
	BatchTimeCutoff:   1 * time.Hour,
	Parallelism:       3,
	ChannelBufferSize: 1,
}

func TestNoVerificationKeySet(t *testing.T) {
	test.FailHandler(t)
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa, metrics.New(false)))

	stream, err := c.Client.StartStream(context.Background())
	Expect(err).To(BeNil())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	_, err = stream.Recv()
	Expect(err).NotTo(BeNil())

	c.TearDown()
}

func TestNoInput(t *testing.T) {
	test.FailHandler(t)
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa, metrics.New(false)))

	_, verificationKey := sigverification_test.GetSignatureFactory(signature.Ecdsa).NewKeys()

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.Client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	output := sigverification_test.OutputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(testTimeout).ShouldNot(Receive())

	c.TearDown()
}

func TestMinimalInput(t *testing.T) {
	test.FailHandler(t)
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa, metrics.New(false)))
	factory := sigverification_test.GetSignatureFactory(signature.Ecdsa)
	signingKey, verificationKey := factory.NewKeys()
	txSigner, _ := factory.NewSigner(signingKey)

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.Client.StartStream(context.Background())
	var emptyData []token.SerialNumber
	emptyDataSig, _ := txSigner.SignTx(emptyData)
	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: &token.Tx{Signature: emptyDataSig, SerialNumbers: emptyData}},
		{BlockNum: 1, TxNum: 2, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}}},
		{BlockNum: 1, TxNum: 3, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}}},
	}})
	Expect(err).To(BeNil())

	output := sigverification_test.OutputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(1 * time.Second).Should(Receive(HaveLen(3)))

	c.TearDown()
}
