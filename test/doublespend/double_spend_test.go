package cluster_test

import (
	"crypto/sha256"
	"fmt"
	"log"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/protos/token"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/test/cluster"
)

func TestMain(t *testing.T) {
	RegisterFailHandler(Fail)
	// RunSpecs(t, "Main Suite")
}

var _ = Describe("Cluster", func() {
	var c *cluster.Cluster

	BeforeEach(func() {
		cConfig := &cluster.ClusterConfig{
			NumSigVerifiers:   4,
			NumShardServers:   4,
			NumShardPerServer: 1,
			SigProfile: signature.Profile{
				Scheme: signature.Ecdsa,
			},
		}

		c = cluster.NewCluster(cConfig)
	})

	AfterEach(func() {
		c.Stop()
	})

	It("all transactions in a block is conflicting with the first transaction on all serial numbers", func() {
		h := sha256.New()

		serialNumbers := [][]byte{}

		for i := 1; i <= 4; i++ {
			h.Write([]byte(strconv.Itoa(i)))
			serialNumbers = append(serialNumbers, h.Sum(nil))
		}

		sign, err := c.TxSigner.SignTx(serialNumbers, nil)
		Expect(err).NotTo(HaveOccurred())

		txs := []*token.Tx{}

		for i := 0; i < 3; i++ {
			txs = append(txs, &token.Tx{
				SerialNumbers: serialNumbers,
				Outputs:       [][]byte{},
				Signature:     sign,
			})
		}

		blk := &token.Block{
			Number: 0,
			Txs:    txs,
		}

		blockStream := c.GetBlockProcessingStream()
		err = blockStream.Send(blk)
		Expect(err).NotTo(HaveOccurred())

		expectedTxStatus := map[token.TxSeqNum]*coordinatorservice.TxValidationStatus{
			{0, 0}: {
				BlockNum: 0,
				TxNum:    0,
				Status:   coordinatorservice.Status_VALID,
			},
			{0, 1}: {
				BlockNum: 0,
				TxNum:    1,
				Status:   coordinatorservice.Status_DOUBLE_SPEND,
			},
			{0, 2}: {
				BlockNum: 0,
				TxNum:    2,
				Status:   coordinatorservice.Status_DOUBLE_SPEND,
			},
		}

		validateStatus(expectedTxStatus, blockStream)
	})

	It("all transactions in a block is conflicting with the first transaction on limited serial numbers", func() {
		h := sha256.New()

		serialNumbers := [][]byte{}

		for i := 0; i < 3; i++ {
			h.Write([]byte(strconv.Itoa(i)))
			serialNumbers = append(serialNumbers, h.Sum(nil))
		}

		allSnSign, err := c.TxSigner.SignTx(serialNumbers, nil)
		Expect(err).NotTo(HaveOccurred())

		sn1Sign, err := c.TxSigner.SignTx(serialNumbers[0:1], nil)
		Expect(err).NotTo(HaveOccurred())

		sn2Sign, err := c.TxSigner.SignTx(serialNumbers[1:2], nil)
		Expect(err).NotTo(HaveOccurred())

		sn3Sign, err := c.TxSigner.SignTx(serialNumbers[2:], nil)
		Expect(err).NotTo(HaveOccurred())

		txs := []*token.Tx{
			{
				SerialNumbers: serialNumbers,
				Outputs:       [][]byte{},
				Signature:     allSnSign,
			},
			{
				SerialNumbers: serialNumbers[:1],
				Outputs:       [][]byte{},
				Signature:     sn1Sign,
			},
			{
				SerialNumbers: serialNumbers[1:2],
				Outputs:       [][]byte{},
				Signature:     sn2Sign,
			},
			{
				SerialNumbers: serialNumbers[2:],
				Outputs:       [][]byte{},
				Signature:     sn3Sign,
			},
		}

		blk := &token.Block{
			Number: 0,
			Txs:    txs,
		}

		blockStream := c.GetBlockProcessingStream()
		err = blockStream.Send(blk)
		Expect(err).NotTo(HaveOccurred())

		expectedTxStatus := map[token.TxSeqNum]*coordinatorservice.TxValidationStatus{
			{0, 0}: {
				Status: coordinatorservice.Status_VALID,
			},
			{0, 1}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
			{0, 2}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
			{0, 3}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
		}

		validateStatus(expectedTxStatus, blockStream)
	})

	It("next block is conflicting fully with the last committed block", func() {
		txs := []*token.Tx{}

		for i := 0; i < 3; i++ {
			h := sha256.New()
			h.Write([]byte(strconv.Itoa(i)))
			sn := h.Sum(nil)

			serialNumbers := [][]byte{sn}

			sign, err := c.TxSigner.SignTx(serialNumbers, nil)
			Expect(err).NotTo(HaveOccurred())

			txs = append(txs, &token.Tx{
				SerialNumbers: serialNumbers,
				Outputs:       [][]byte{},
				Signature:     sign,
			})
		}

		blk := &token.Block{
			Number: 0,
			Txs:    txs,
		}

		blockStream := c.GetBlockProcessingStream()
		err := blockStream.Send(blk)
		Expect(err).NotTo(HaveOccurred())

		expectedTxStatus := map[token.TxSeqNum]*coordinatorservice.TxValidationStatus{
			{0, 0}: {
				Status: coordinatorservice.Status_VALID,
			},
			{0, 1}: {
				Status: coordinatorservice.Status_VALID,
			},
			{0, 2}: {
				Status: coordinatorservice.Status_VALID,
			},
		}

		validateStatus(expectedTxStatus, blockStream)

		blk.Number = 1
		err = blockStream.Send(blk)
		Expect(err).NotTo(HaveOccurred())

		expectedTxStatus = map[token.TxSeqNum]*coordinatorservice.TxValidationStatus{
			{1, 0}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
			{1, 1}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
			{1, 2}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
		}

	})

	It("next transaction in a block is conflicting with the previous transaction", func() {
		txs := []*token.Tx{}

		for i := 0; i < 5; i++ {
			h := sha256.New()
			h.Write([]byte(strconv.Itoa(i)))
			sn1 := h.Sum(nil)
			h = sha256.New()
			h.Write([]byte(strconv.Itoa(i + 1)))
			sn2 := h.Sum(nil)

			serialNumbers := [][]byte{sn1, sn2}

			sign, err := c.TxSigner.SignTx(serialNumbers, nil)
			Expect(err).NotTo(HaveOccurred())

			txs = append(txs, &token.Tx{
				SerialNumbers: serialNumbers,
				Outputs:       [][]byte{},
				Signature:     sign,
			})
		}

		blk := &token.Block{
			Number: 0,
			Txs:    txs,
		}

		blockStream := c.GetBlockProcessingStream()
		err := blockStream.Send(blk)
		Expect(err).NotTo(HaveOccurred())

		expectedTxStatus := map[token.TxSeqNum]*coordinatorservice.TxValidationStatus{
			{0, 0}: {
				Status: coordinatorservice.Status_VALID,
			},
			{0, 1}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
			{0, 2}: {
				Status: coordinatorservice.Status_VALID,
			},
			{0, 3}: {
				Status: coordinatorservice.Status_DOUBLE_SPEND,
			},
			{0, 4}: {
				Status: coordinatorservice.Status_VALID,
			},
		}

		validateStatus(expectedTxStatus, blockStream)
	})
})

func validateStatus(expectedStatus map[token.TxSeqNum]*coordinatorservice.TxValidationStatus, blockStream coordinatorservice.Coordinator_BlockProcessingClient) {
	processed := 0
	for {
		status, err := blockStream.Recv()
		Expect(err).NotTo(HaveOccurred())

		for i := 0; i < len(status.TxsValidationStatus); i++ {
			txSeqNum := token.TxSeqNum{
				BlkNum: status.TxsValidationStatus[i].BlockNum,
				TxNum:  status.TxsValidationStatus[i].TxNum,
			}

			expStatus := expectedStatus[txSeqNum]
			log.Printf("TxSeqNum: %s, ExpectedStatus: %s, Actual Status: %s", txSeqNum, expStatus.Status.String(), status.TxsValidationStatus[i].Status.String())
			fmt.Println(txSeqNum, expStatus, status.TxsValidationStatus[i])
			Expect(status.TxsValidationStatus[i].Status).To(Equal(expStatus.Status))
		}

		processed += len(status.TxsValidationStatus)
		if processed == len(expectedStatus) {
			break
		}
	}
}
