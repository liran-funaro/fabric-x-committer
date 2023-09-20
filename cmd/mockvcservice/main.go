package main

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

var configPath string

func main() {
	cmd := mockvcserviceCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func mockvcserviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mockvcservice",
		Short: "mockvcservice is a mock validator and committer service.",
	}
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a mockvcservice",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configs must be set")
			}

			cmd.SilenceUsage = true
			if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
				return err
			}
			config := vcservice.ReadConfig()

			vcs := vcservicemock.NewMockVcService()
			connection.RunServerMain(config.Server, func(grpcServer *grpc.Server, _ int) {
				protovcservice.RegisterValidationAndCommitServiceServer(grpcServer, vcs)
			})
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
