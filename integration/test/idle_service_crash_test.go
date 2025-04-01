package test

import (
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
)

//nolint:paralleltest
func TestCrashWhenIdle(t *testing.T) { //nolint:gocognit
	gomega.RegisterTestingT(t)
	c := runner.NewRuntime(t, &runner.Config{
		NumVerifiers: 2,
		NumVCService: 2,
		BlockSize:    5,
		BlockTimeout: 2 * time.Second,
	})
	c.StartSystem(t, runner.All)
	c.CreateNamespacesAndCommit(t, "1")

	// Scenario:
	// 1. Submit two transactions and verify their commitment.
	// 2. Terminate one or more services (excluding the ordering service).
	// 3. Resubmit the same transactions to the ordering service while some services remain offline.
	// 4. Restart the terminated services and confirm that all transactions are processed successfully,
	//    with the client receiving duplicate transaction ID statuses.
	txs := []*protoblocktx.Tx{
		{
			Id: "tx1",
			Namespaces: []*protoblocktx.TxNamespace{
				{
					BlindWrites: []*protoblocktx.Write{
						{
							Key: []byte("k1"),
						},
					},
				},
			},
		},
		{
			Id: "tx2",
			Namespaces: []*protoblocktx.TxNamespace{
				{
					BlindWrites: []*protoblocktx.Write{
						{
							Key: []byte("k2"),
						},
					},
				},
			},
		},
	}

	i := 1
	t.Logf("\n%d. Send transactions before stopping services and verify their commitment.\n", i)
	addSignAndSendTransactions(t, c, txs)

	expectedResults := &runner.ExpectedStatusInBlock{
		TxIDs: []string{"tx1", "tx2"},
		Statuses: []protoblocktx.Status{
			protoblocktx.Status_COMMITTED,
			protoblocktx.Status_COMMITTED,
		},
	}
	c.ValidateExpectedResultsInCommittedBlock(t, expectedResults)

	for _, serviceNames := range [][]string{
		{"coordinator"},
		{"verifiers"},
		{"vcservices"},
		{"sidecar"},
		{"coordinator", "sidecar"},
		{"vcservices", "verifiers"},
		// We fail all four services, but we test the order too.
		// However, the order of vcservices and verifiers do not matter.
		{"coordinator", "vcservices", "verifiers", "sidecar"},
		{"verifiers", "vcservices", "coordinator", "sidecar"},
		{"sidecar", "vcservices", "verifiers", "coordinator"},
	} {
		services := strings.Join(serviceNames, "-")
		i++
		t.Logf("\n%d. Stop "+services+"\n", i)
		for _, s := range serviceNames {
			switch s {
			case "coordinator":
				c.Coordinator.Stop(t)
			case "verifiers":
				for _, verifier := range c.Verifier {
					verifier.Stop(t)
				}
			case "vcservices":
				for _, vcservice := range c.VcService {
					vcservice.Stop(t)
				}
			case "sidecar":
				c.Sidecar.Stop(t)
			}
			time.Sleep(5 * time.Second) // between failures, wait for 5 seconds
		}

		i++
		t.Logf("\n%d. Send transactions after stopping "+services+"\n", i)
		addSignAndSendTransactions(t, c, txs)

		time.Sleep(10 * time.Second) // allow time for the transactions to reach orderer and create a block

		i++
		t.Logf("\n%d. Restart "+services+"\n", i)
		for _, s := range serviceNames {
			switch s {
			case "coordinator":
				c.Coordinator.Restart(t)
			case "verifiers":
				for _, verifier := range c.Verifier {
					verifier.Restart(t)
				}
			case "vcservices":
				for _, vcservice := range c.VcService {
					vcservice.Restart(t)
				}
			case "sidecar":
				c.Sidecar.Restart(t)
			}
			time.Sleep(5 * time.Second) // between restart, wait for 5 seconds
		}

		i++
		t.Logf("\n%d. Ensure that the last block is committed after restarting "+services+"\n", i)
		expectedResults = &runner.ExpectedStatusInBlock{
			TxIDs: []string{"tx1", "tx2"},
			Statuses: []protoblocktx.Status{
				protoblocktx.Status_ABORTED_DUPLICATE_TXID,
				protoblocktx.Status_ABORTED_DUPLICATE_TXID,
			},
		}
		c.ValidateExpectedResultsInCommittedBlock(t, expectedResults)
	}
}

func addSignAndSendTransactions(t *testing.T, c *runner.CommitterRuntime, txs []*protoblocktx.Tx) {
	t.Helper()
	v0 := types.VersionNumber(0).Bytes()
	for _, tx := range txs {
		for _, ns := range tx.Namespaces {
			ns.NsId = "1"
			ns.NsVersion = v0
		}
		c.AddSignatures(t, tx)
	}
	c.SendTransactionsToOrderer(t, txs)
}
