package main

import (
	"context"
	"time"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/prometheusmetrics"
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
	samplingTicker := time.NewTicker(10 * time.Second)
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
			select {
			case <-samplingTicker.C:
				t := time.Now()
				for _, tx := range blk.Txs {
					latencyTracker.Store(tx.Id, t)
				}
			default:
			}
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

		for id := range txStatus.TxsValidationStatus {
			if t, ok := latencyTracker.LoadAndDelete(id); ok {
				start, _ := t.(time.Time)
				prometheusmetrics.Observe(metrics.transactionLatencySecond, time.Since(start))
			}
		}
	}
}
