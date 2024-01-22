package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/queryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

var configPath string

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
		Use:   "queryservice",
		Short: "queryservice is a service to query the state.",
	}
	cmd.AddCommand(versionCmd())
	cmd.AddCommand(startCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of the queryservice.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("trailing arguments detected")
			}

			cmd.SilenceUsage = true
			cmd.Println("queryservice 0.1")

			return nil
		},
	}

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a queryservice",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return errors.New("--configs flag must be set to the path of configuration file")
			}

			if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
				return err
			}
			serviceConfig := queryservice.ReadConfig()
			cmd.SilenceUsage = true

			cmd.Println("Starting queryservice")
			qs, err := queryservice.NewQueryService(serviceConfig)
			if err != nil {
				return err
			}
			defer qs.Close()

			promErr := qs.StartPrometheusServer()

			go connection.RunServerMain(serviceConfig.Server, func(server *grpc.Server, port int) {
				if serviceConfig.Server.Endpoint.Port == 0 {
					serviceConfig.Server.Endpoint.Port = port
				}
				protoqueryservice.RegisterQueryServiceServer(server, qs)
			})

			err = getCmdErr(cmd.Context(), promErr)
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	return cmd
}

func getCmdErr(ctx context.Context, errChan <-chan error) error {
	for {
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case err := <-errChan:
			if err != nil {
				return err
			}
		}
	}
}
