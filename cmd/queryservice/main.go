package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/queryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

const (
	serviceName    = "query-service"
	serviceVersion = "0.0.1"
)

func main() {
	cmd := queryServiceCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func queryServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a service to query the state.", serviceName),
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
			conf := queryservice.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			qs := queryservice.NewQueryService(conf)
			return connection.StartService(cmd.Context(), qs, conf.Server, func(server *grpc.Server) {
				protoqueryservice.RegisterQueryServiceServer(server, qs)
			})
		},
	}
	cobracmd.SetDefaultFlags(cmd, serviceName, &configPath)
	return cmd
}
