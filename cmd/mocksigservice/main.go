package main

import (
	"errors"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

var configPath string

func main() {
	cmd := mocksigverifierserviceCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func mocksigverifierserviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mocksigverifierservice",
		Short: "mocksigverifierservice is a mock signature verification service.",
	}
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a mocksigverifierservice",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configs must be set")
			}

			cmd.SilenceUsage = true
			if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
				return err
			}
			config := serverconfig.ReadConfig()

			sv := sigverifiermock.NewMockSigVerifier()
			connection.RunServerMain(config.Server, func(grpcServer *grpc.Server, _ int) {
				protosigverifierservice.RegisterVerifierServer(grpcServer, sv)
			})
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
