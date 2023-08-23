package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

var configPath string

func main() {
	cmd := vcserviceCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func vcserviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vcservice",
		Short: "vcservice is a validator and committer service.",
	}
	cmd.AddCommand(versionCmd())
	cmd.AddCommand(startCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of the vcservice.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("trailing arguments detected")
			}

			cmd.SilenceUsage = true
			cmd.Println("vcservice 0.2")

			return nil
		},
	}

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a vcservice",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configpath flag must be set to the path of configuration file")
			}

			if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
				return err
			}
			vcConfig := vcservice.ReadConfig()
			cmd.SilenceUsage = true

			cmd.Println("Starting vcservice")
			vc, err := vcservice.NewValidatorCommitterService(vcConfig)
			if err != nil {
				return err
			}

			connection.RunServerMain(vcConfig.Server, func(server *grpc.Server, port int) {
				if vcConfig.Server.Endpoint.Port == 0 {
					vcConfig.Server.Endpoint.Port = port
				}
				protovcservice.RegisterValidationAndCommitServiceServer(server, vc)
			})
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configpath", "", "set the absolute path of config directory")
	return cmd
}
