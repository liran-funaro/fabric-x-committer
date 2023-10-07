package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

func generateLoadForVCService(
	cmd *cobra.Command,
	c *BlockgenConfig,
	blockGen *loadgen.BlockStreamGenerator,
) error {
	errChan := make(chan error, len(c.VCServiceEndpoints))

	for _, endpoint := range c.VCServiceEndpoints {
		cmd.Printf("Connecting to %s\n", endpoint.String())
		conn, err := connection.Connect(connection.NewDialConfig(*endpoint))
		if err != nil {
			return err
		}

		client := protovcservice.NewValidationAndCommitServiceClient(conn)
		csStream, err := client.StartValidateAndCommitStream(context.Background())
		if err != nil {
			return err
		}

		go func() {
			errChan <- sendTransactions(
				blockGen,
				csStream,
				c.RateLimit/len(c.VCServiceEndpoints),
				c.LatencySamplingInterval,
			)
		}()

		go func() {
			errChan <- receiveStatusFromVCService(cmd, csStream)
		}()
	}

	cmd.Println("blockgen started")

	return <-errChan
}

func receiveStatusFromVCService(
	cmd *cobra.Command,
	csStream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) error {
	cmd.Println("Start receiving status from vc service")
	for {
		txStatus, err := csStream.Recv()
		if err != nil {
			fmt.Println("receive tx:", err)
			return err
		}

		metrics.addToCounter(metrics.transactionReceivedTotal, len(txStatus.Status))

		for id, status := range txStatus.Status {
			recordLatency(id, status)
		}
	}
}

func recordLatency(txID string, status protoblocktx.Status) {
	if t, ok := latencyTracker.LoadAndDelete(txID); ok {
		start, _ := t.(time.Time)
		elapsed := time.Since(start)

		if status == protoblocktx.Status_COMMITTED {
			prometheusmetrics.Observe(metrics.validTransactionLatencySecond, elapsed)
		} else {
			prometheusmetrics.Observe(metrics.invalidTransactionLatencySecond, elapsed)
		}
	}
}
