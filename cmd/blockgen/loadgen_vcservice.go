package main

import (
	"context"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
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
			errChan <- sendTransactionsToVCService(cmd, blockGen, csStream)
		}()

		go func() {
			errChan <- receiveStatusFromVCService(cmd, csStream)
		}()
	}

	cmd.Println("blockgen started")

	return <-errChan
}

func sendTransactionsToVCService(
	cmd *cobra.Command,
	blockGen *loadgen.BlockStreamGenerator,
	csStream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) error {
	cmd.Println("Start sending transactions to vc service")
	stopSender = make(chan any)
	for {
		select {
		case <-stopSender:
			return nil
		default:
			blk := <-blockGen.BlockQueue

			txBatch := &protovcservice.TransactionBatch{}
			for _, tx := range blk.Txs {
				txBatch.Transactions = append(
					txBatch.Transactions,
					&protovcservice.Transaction{
						ID:         tx.Id,
						Namespaces: tx.Namespaces,
					},
				)
			}
			if err := csStream.Send(txBatch); err != nil {
				return err
			}

			metrics.addToCounter(metrics.transactionSentTotal, len(blk.Txs))
		}
	}
}

func receiveStatusFromVCService(
	cmd *cobra.Command,
	csStream protovcservice.ValidationAndCommitService_StartValidateAndCommitStreamClient,
) error {
	cmd.Println("Start receiving status from vc service")
	for {
		txStatus, err := csStream.Recv()
		if err != nil {
			return err
		}

		metrics.addToCounter(metrics.transactionReceivedTotal, len(txStatus.Status))
	}
}
