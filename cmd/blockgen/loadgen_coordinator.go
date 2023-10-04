package main

import (
	"context"

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
		errChan <- sendBlockToCoordinatorService(cmd, blockGen, csStream)
	}()

	go func() {
		errChan <- receiveStatusFromCoordinatorService(cmd, csStream)
	}()

	cmd.Println("blockgen started")

	return <-errChan
}

func sendBlockToCoordinatorService(
	cmd *cobra.Command,
	blockGen *loadgen.BlockStreamGenerator,
	csStream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	cmd.Println("Start sending blocks to coordinator service")
	stopSender = make(chan any)
	for {
		select {
		case <-stopSender:
			return nil
		default:
			blk := <-blockGen.BlockQueue
			if err := csStream.Send(blk); err != nil {
				return err
			}

			metrics.addToCounter(metrics.blockSentTotal, 1)
			metrics.addToCounter(metrics.transactionSentTotal, len(blk.Txs))
		}
	}
}

func receiveStatusFromCoordinatorService(
	cmd *cobra.Command,
	csStream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	cmd.Println("Start receiving status from coordinator service")
	for {
		txStatus, err := csStream.Recv()
		if err != nil {
			return err
		}

		metrics.addToCounter(metrics.transactionReceivedTotal, len(txStatus.TxsValidationStatus))
	}
}
