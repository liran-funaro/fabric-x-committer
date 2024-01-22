package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protocoordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/coordinatorservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

var (
	configPath   string
	grpcServer   *grpc.Server
	coordService *coordinatorservice.CoordinatorService
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
		Use:   "coordinatorservice",
		Short: "coordinatorservice is a coordinator for the scalable committer.",
	}
	cmd.AddCommand(versionCmd())
	cmd.AddCommand(startCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of the coordinatorservice.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("trailing arguments detected")
			}

			cmd.SilenceUsage = true
			cmd.Println("coordinatorservice 0.2")

			return nil
		},
	}

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a coordinatorservice",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configs flag must be set to the path of configuration file")
			}

			if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
				return err
			}
			coordConfig := coordinatorservice.ReadConfig()
			cmd.SilenceUsage = true

			cmd.Println("Starting coordinatorservice")
			coordService = coordinatorservice.NewCoordinatorService(coordConfig)

			sigVerErr, vcErr, err := coordService.Start()
			if err != nil {
				return err
			}

			errChan := make(chan error)
			captureErrFromCoordService(errChan, coordConfig, sigVerErr, vcErr)

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				connection.RunServerMain(coordConfig.ServerConfig, func(server *grpc.Server, port int) {
					if coordConfig.ServerConfig.Endpoint.Port == 0 {
						coordConfig.ServerConfig.Endpoint.Port = port
					}
					protocoordinatorservice.RegisterCoordinatorServer(server, coordService)
					grpcServer = server
					wg.Done()
				})
			}()
			wg.Wait()

			ctx := cmd.Context()
			select {
			case <-ctx.Done():
				err := context.Cause(ctx)
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				return err
			case err := <-errChan:
				// As we do not have recovery mechanism for vcservice and sigverifier service, we stop the
				// coordinator service if any of them fails. In the future, we can add recovery mechanism
				// to restart the failed service and stop the coordinator service only if all the services
				// in vcservice fail or all the services in sigverifier fail.
				cmd.Println("stream with signature verifier or vcservice closed abrubtly")
				return err
			}
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	return cmd
}

func captureErrFromCoordService(
	errChan chan error,
	coordConfig *coordinatorservice.CoordinatorConfig,
	sigVerErr, vcErr chan error,
) {
	go func() {
		numSigVer := len(coordConfig.SignVerifierConfig.ServerConfig)
		for i := 0; i < numSigVer; i++ {
			err := <-sigVerErr
			if err != nil {
				errChan <- err
				break
			}
		}
	}()

	go func() {
		numVC := len(coordConfig.ValidatorCommitterConfig.ServerConfig)
		for i := 0; i < numVC; i++ {
			err := <-vcErr
			if err != nil {
				errChan <- err
				break
			}
		}
	}()
}
