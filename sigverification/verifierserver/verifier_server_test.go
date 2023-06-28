package verifierserver_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/sigverification"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	sigverification_test "github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/test"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/test"
)

const testTimeout = 3 * time.Second

var parallelExecutionConfig = &parallelexecutor.Config{
	BatchSizeCutoff:   3,
	BatchTimeCutoff:   1 * time.Hour,
	Parallelism:       3,
	ChannelBufferSize: 1,
}

func TestNoVerificationKeySet(t *testing.T) {
	t.Skip("Temporarily skipping. Related to commented-out error in StartStream.")
	test.FailHandler(t)
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa, m))

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
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa, m))

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
	m := (&metrics.Provider{}).NewMonitoring(false, &latency.NoOpTracer{}).(*metrics.Metrics)
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, signature.Ecdsa, m))
	factory := sigverification_test.GetSignatureFactory(signature.Ecdsa)
	signingKey, verificationKey := factory.NewKeys()
	txSigner, _ := factory.NewSigner(signingKey)

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{SerializedBytes: verificationKey})
	Expect(err).To(BeNil())

	stream, _ := c.Client.StartStream(context.Background())
	var emptyData [][]byte
	emptyDataSig, _ := txSigner.SignTx(emptyData, emptyData)
	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: &token.Tx{Signature: emptyDataSig, SerialNumbers: emptyData, Outputs: emptyData}},
		{BlockNum: 1, TxNum: 2, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}, Outputs: [][]byte{}}},
		{BlockNum: 1, TxNum: 3, Tx: &token.Tx{Signature: []byte{}, SerialNumbers: [][]byte{}, Outputs: [][]byte{}}},
	}})
	Expect(err).To(BeNil())

	output := sigverification_test.OutputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(1 * time.Second).Should(Receive(HaveLen(3)))

	c.TearDown()
}
