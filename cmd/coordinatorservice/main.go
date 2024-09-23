package main

import (
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

const (
	serviceName    = "coordinator-service"
	serviceVersion = "0.0.2"
)

func main() {
	cmd := coordinatorserviceCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func coordinatorserviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a coordinator for the scalable committer.", serviceName),
	}
	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command { //nolint:gocognit
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
			coordConfig := coordinatorservice.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			coordService := coordinatorservice.NewCoordinatorService(coordConfig)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				connection.RunServerMain(coordConfig.ServerConfig, func(server *grpc.Server, port int) {
					if coordConfig.ServerConfig.Endpoint.Port == 0 {
						coordConfig.ServerConfig.Endpoint.Port = port
					}
					protocoordinatorservice.RegisterCoordinatorServer(server, coordService)
					wg.Done()
				})
			}()
			wg.Wait()

			// As we do not have recovery mechanism for vcservice and sigverifier service, we stop the
			// coordinator service if any of them fails. In the future, we can add recovery mechanism
			// to restart the failed service and stop the coordinator service only if all the services
			// in vcservice fail or all the services in sigverifier fail.
			return coordService.Run(cmd.Context())
		},
	}
	cobracmd.SetDefaultFlags(cmd, serviceName, &configPath)
	return cmd
}
