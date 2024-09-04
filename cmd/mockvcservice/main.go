package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/vcservicemock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

const (
	serviceName    = "mockvcservice"
	serviceVersion = "0.0.1"
)

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
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a mock validator and committer service.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Starts a %v.", serviceName),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			vcConfig := vcservice.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			vcs := vcservicemock.NewMockVcService()
			go connection.RunServerMain(vcConfig.Server, func(grpcServer *grpc.Server, _ int) {
				protovcservice.RegisterValidationAndCommitServiceServer(grpcServer, vcs)
			})

			return cobracmd.WaitUntilServiceDone(cmd.Context())
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
