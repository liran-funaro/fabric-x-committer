package verifierserver_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/testutils"
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
	c := testutils.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa))

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
	c := testutils.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa))

	_, verificationKey := signature.NewSignerPubKey(signature.Ecdsa)

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.Client.StartStream(context.Background())

	err = stream.Send(&sigverification.RequestBatch{})
	Expect(err).To(BeNil())

	output := testutils.OutputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(testTimeout).ShouldNot(Receive())

	c.TearDown()
}

func TestMinimalInput(t *testing.T) {
	test.FailHandler(t)
	c := testutils.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa))

	txSigner, verificationKey := signature.NewSignerPubKey(signature.Ecdsa)

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.Client.StartStream(context.Background())
	var emptyData []signature.SerialNumber
	emptyDataSig, _ := txSigner.SignTx(emptyData)
	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: &token.Tx{Signature: emptyDataSig, SerialNumbers: emptyData}},
		{BlockNum: 1, TxNum: 2, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}}},
		{BlockNum: 1, TxNum: 3, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}}},
	}})
	Expect(err).To(BeNil())

	output := testutils.OutputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(1 * time.Second).Should(Receive(HaveLen(3)))

	c.TearDown()
}
