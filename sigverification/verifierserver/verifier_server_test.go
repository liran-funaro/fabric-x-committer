package verifierserver_test

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
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
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, m))

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
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, m))

	_, verificationKey := sigverification_test.GetSignatureFactory(signature.Ecdsa).NewKeys()

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{
		NsId:            1,
		NsVersion:       types.VersionNumber(0).Bytes(),
		SerializedBytes: verificationKey,
		Scheme:          signature.Ecdsa,
	})
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
	c := sigverification_test.NewTestState(verifierserver.New(parallelExecutionConfig, m))
	factory := sigverification_test.GetSignatureFactory(signature.Ecdsa)
	signingKey, verificationKey := factory.NewKeys()
	txSigner, _ := factory.NewSigner(signingKey)

	_, err := c.Client.SetVerificationKey(context.Background(), &sigverification.Key{
		NsId:            1,
		NsVersion:       types.VersionNumber(0).Bytes(),
		SerializedBytes: verificationKey,
		Scheme:          signature.Ecdsa,
	})
	Expect(err).To(BeNil())

	stream, _ := c.Client.StartStream(context.Background())

	tx1 := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      1,
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0001"),
			}},
		}},
	}
	s, _ := txSigner.SignNs(tx1, 0)
	tx1.Signatures = append(tx1.Signatures, s)

	tx2 := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      1,
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0010"),
			}},
		}},
	}

	s, _ = txSigner.SignNs(tx2, 0)
	tx2.Signatures = append(tx2.Signatures, s)

	tx3 := &protoblocktx.Tx{
		Namespaces: []*protoblocktx.TxNamespace{{
			NsId:      1,
			NsVersion: types.VersionNumber(0).Bytes(),
			BlindWrites: []*protoblocktx.Write{{
				Key: []byte("0011"),
			}},
		}},
	}
	s, _ = txSigner.SignNs(tx3, 0)
	tx3.Signatures = append(tx3.Signatures, s)

	err = stream.Send(&sigverification.RequestBatch{Requests: []*sigverification.Request{
		{BlockNum: 1, TxNum: 1, Tx: tx1},
		{BlockNum: 1, TxNum: 2, Tx: tx2},
		{BlockNum: 1, TxNum: 3, Tx: tx3},
	}})
	Expect(err).To(BeNil())

	output := sigverification_test.OutputChannel(stream)
	Expect(err).To(BeNil())
	Eventually(output).WithTimeout(1 * time.Second).Should(Receive(HaveLen(3)))

	c.TearDown()
}
