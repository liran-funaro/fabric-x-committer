package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

var (
	configPath string
	metrics    *perfMetrics
)

// BlockgenConfig is the configuration for blockgen.
type BlockgenConfig struct {
	CoordinatorEndpoint *connection.Endpoint `mapstructure:"coordinator-endpoint"`
	TxRatePerSecond     int                  `mapstructure:"tx-rate-per-second"`
	Monitoring          *monitoring.Config   `mapstructure:"monitoring"`
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

			conn, err := connection.Connect(connection.NewDialConfig(*c.CoordinatorEndpoint))
			if err != nil {
				return err
			}

			client := protocoordinatorservice.NewCoordinatorClient(conn)
			csStream, err := client.BlockProcessing(context.Background())
			if err != nil {
				return err
			}

			profile := loadgen.LoadProfileFromYaml(configPath)
			blockGen := loadgen.StartBlockGenerator(profile)
			errChan := make(chan error)

			if profile.Block.Size > int64(c.TxRatePerSecond) {
				return fmt.Errorf(
					"block size (%d) cannot be larger than tx rate per second (%d)",
					profile.Block.Size,
					c.TxRatePerSecond,
				)
			}

			go func() {
				blockRate := c.TxRatePerSecond / int(profile.Block.Size)
				errChan <- sendBlockToCoordinatorService(cmd, blockGen, csStream, blockRate)
			}()

			go func() {
				errChan <- receiveStatusFromCoordinatorService(cmd, csStream)
			}()

			cmd.Println("blockgen started")

			return <-errChan
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	return cmd
}

func sendBlockToCoordinatorService(
	cmd *cobra.Command,
	blockGen *loadgen.BlockStreamGenerator,
	csStream protocoordinatorservice.Coordinator_BlockProcessingClient,
	blockRate int,
) error {
	cmd.Println("Start sending blocks to coordinator service")
	sleepTime := time.Duration(1000/blockRate)*time.Millisecond - 1
	for {
		blk := <-blockGen.BlockQueue
		if err := csStream.Send(blk); err != nil {
			return err
		}

		metrics.addToCounter(metrics.blockSentTotal, 1)
		metrics.addToCounter(metrics.transactionSentTotal, len(blk.Txs))
		time.Sleep(sleepTime)
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
