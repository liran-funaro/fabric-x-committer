package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

var (
	configPath string
	component  string
	metrics    *perfMetrics
	stopSender chan any
)

// BlockgenConfig is the configuration for blockgen.
type BlockgenConfig struct {
	CoordinatorEndpoint *connection.Endpoint   `mapstructure:"coordinator-endpoint"`
	VCServiceEndpoints  []*connection.Endpoint `mapstructure:"vcservice-endpoints"`
	Monitoring          *monitoring.Config     `mapstructure:"monitoring"`
}

func main() {
	cmd := blockgenCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func blockgenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blockgen",
		Short: "blockgen is a block generator for coordinator service.",
	}
	cmd.AddCommand(versionCmd())
	cmd.AddCommand(startCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of the blockgen.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("trailing arguments detected")
			}

			cmd.SilenceUsage = true
			cmd.Println("blockgen 0.2")

			return nil
		},
	}

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a blockgen",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configs flag must be set to the path of configuration file")
			}

			if component == "" {
				return errors.New("--component flag must be set to the component name for which load is generated")
			}

			c, err := readConfig()
			if err != nil {
				return err
			}

			metrics = newBlockgenServiceMetrics(c.Monitoring.Metrics.Enable)
			var promErrChan <-chan error
			if metrics.enabled {
				promErrChan = metrics.provider.StartPrometheusServer(c.Monitoring.Metrics.Endpoint)
			}

			go func() {
				if errProm := <-promErrChan; errProm != nil {
					log.Panic(err) // nolint: revive
				}
			}()

			profile := loadgen.LoadProfileFromYaml(configPath)
			blockGen := loadgen.StartBlockGenerator(profile)

			switch component {
			case "coordinator":
				err = generateLoadForCoordinatorService(cmd, c, blockGen)
			case "vcservice":
				err = generateLoadForVCService(cmd, c, blockGen)
			default:
				err = fmt.Errorf("invalid component name: %s", component)
			}

			return err
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.PersistentFlags().StringVar(&component, "component", "", "set the component name for which load is generated")
	return cmd
}

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

func generateLoadForVCService(
	cmd *cobra.Command,
	c *BlockgenConfig,
	blockGen *loadgen.BlockStreamGenerator,
) error {
	conn, err := connection.Connect(connection.NewDialConfig(*c.VCServiceEndpoints[0]))
	if err != nil {
		return err
	}

	client := protovcservice.NewValidationAndCommitServiceClient(conn)
	csStream, err := client.StartValidateAndCommitStream(context.Background())
	if err != nil {
		return err
	}

	errChan := make(chan error)

	go func() {
		errChan <- sendTransactionsToVCService(cmd, blockGen, csStream)
	}()

	go func() {
		errChan <- receiveStatusFromVCService(cmd, csStream)
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

func readConfig() (*BlockgenConfig, error) {
	if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
		return nil, err
	}
	wrapper := new(struct {
		Config BlockgenConfig `mapstructure:"blockgen"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config, nil
}
