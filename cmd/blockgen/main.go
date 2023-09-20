package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

var configPath string

// CoordinatorServiceEndpoint is the configuration for coordinator service endpoint.
type CoordinatorServiceEndpoint struct {
	Endpoint *connection.Endpoint `mapstructure:"endpoint"`
}

func main() {
	cmd := blockgenForCoordinatorCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func blockgenForCoordinatorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blockgenforcoordinator",
		Short: "blockgenforcoordinator is a block generator for coordinator service.",
	}
	cmd.AddCommand(versionCmd())
	cmd.AddCommand(startCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of the blockgenforcoordinator.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("trailing arguments detected")
			}

			cmd.SilenceUsage = true
			cmd.Println("blockgenforcoordinator 0.2")

			return nil
		},
	}

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a blockgenforcoordinator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configs flag must be set to the path of configuration file")
			}

			if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
				return err
			}
			wrapper := new(struct {
				Config CoordinatorServiceEndpoint `mapstructure:"coordinator-service"`
			})
			config.Unmarshal(wrapper)
			csEndpoint := wrapper.Config.Endpoint

			profile := loadgen.LoadProfileFromYaml(configPath)
			blockGen := loadgen.StartBlockGenerator(profile)

			conn, err := connection.Connect(connection.NewDialConfig(*csEndpoint))
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

			cmd.Println("blockgenforcoordinator started")

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
) error {
	cmd.Println("Sending block to the coordinator service")
	for {
		blk := <-blockGen.BlockQueue
		if err := csStream.Send(blk); err != nil {
			return err
		}
	}
}

func receiveStatusFromCoordinatorService(
	cmd *cobra.Command,
	csStream protocoordinatorservice.Coordinator_BlockProcessingClient,
) error {
	cmd.Println("Received status from the coordinator service")
	for {
		if _, err := csStream.Recv(); err != nil {
			return err
		}
	}
}
