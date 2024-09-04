package main

import (
	"fmt"
	"os"

	"github.com/hyperledger/fabric-protos-go/peer"
	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	serviceName    = "sidecar"
	serviceVersion = "0.0.1"
)

func main() {
	cmd := sidecarCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func sidecarCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   serviceName,
		Short: fmt.Sprintf("%v is the linkage between the system services.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Starts a %v service.", serviceName),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			c := sidecar.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			service, err := sidecar.New(&c)
			utils.Must(err)

			// enable health check server
			healthcheck := health.NewServer()
			healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)

			go func() {
				connection.RunServerMain(c.Server, func(server *grpc.Server, _ int) {
					peer.RegisterDeliverServer(server, service.GetLedgerService())
					healthgrpc.RegisterHealthServer(server, healthcheck)
				})
			}()
			return service.Run(cmd.Context())
		},
	}
	cobracmd.SetDefaultFlags(cmd, serviceName, &configPath)
	setFlags(cmd)
	return cmd
}

// setFlags setting the relevant flags for the service usage.
func setFlags(cmd *cobra.Command) {
	cobracmd.CobraInt(
		cmd,
		"orderer-endpoint",
		"sets the endpoint of the orderer in the config file",
		fmt.Sprintf("%v.orderer.endpoint", serviceName),
	)

	cobracmd.CobraInt(
		cmd,
		"committer-endpoint",
		"sets the endpoint of the committer in the config file",
		fmt.Sprintf("%v.committer.endpoint", serviceName),
	)

	cobracmd.CobraString(
		cmd,
		"ledger-path",
		"sets the path of the ledger",
		fmt.Sprintf("%v.ledger.path", serviceName),
	)
}
