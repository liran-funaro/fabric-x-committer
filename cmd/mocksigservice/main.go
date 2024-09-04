package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice/sigverifiermock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

const (
	serviceName    = "mocksigverifierservice"
	serviceVersion = "0.0.1"
)

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
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a mock signature verification service.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Starts a %v", serviceName),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			sigConfig := serverconfig.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			sv := sigverifiermock.NewMockSigVerifier()
			go connection.RunServerMain(sigConfig.Server, func(grpcServer *grpc.Server, _ int) {
				protosigverifierservice.RegisterVerifierServer(grpcServer, sv)
			})

			return cobracmd.WaitUntilServiceDone(cmd.Context())
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
