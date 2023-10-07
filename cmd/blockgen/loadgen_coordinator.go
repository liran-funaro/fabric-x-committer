package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

func generateLoadForCoordinatorService(
	cmd *cobra.Command,
	c *BlockgenConfig,
	blockGen *loadgen.BlockStreamGenerator,
) error {
	conn, err := connection.Connect(connection.NewDialConfig(*c.CoordinatorEndpoint))
	if err != nil {
		return err
	}

	client := protocoordinatorservice.NewCoordinatorClient(conn)

	pubKey := blockGen.Signer.GetVerificationKey()
	_, err = client.SetVerificationKey(
		context.Background(),
		&protosigverifierservice.Key{SerializedBytes: pubKey},
	)
	if err != nil {
		return err
	}

	csStream, err := client.BlockProcessing(context.Background())
	if err != nil {
		return err
	}

	errChan := make(chan error)

	go func() {
		errChan <- sendTransactions(blockGen, csStream, c.RateLimit, c.LatencySamplingInterval)
	}()

	go func() {
		errChan <- receiveStatusFromCoordinatorService(cmd, csStream)
	}()

	return <-errChan
}

func receiveStatusFromCoordinatorService(
	cmd *cobra.Command,
	csStream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	cmd.Println("Start receiving status from coordinator service")
	for {
		txStatus, err := csStream.Recv()
		if err != nil {
			fmt.Println("receive tx:", err)
			return err
		}

		metrics.addToCounter(metrics.transactionReceivedTotal, len(txStatus.TxsValidationStatus))

		for _, tx := range txStatus.TxsValidationStatus {
			recordLatency(tx.TxId, tx.Status)
		}
	}
}
