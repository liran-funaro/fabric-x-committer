package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
	"google.golang.org/grpc"
)

const (
	serviceName    = "validator-committer-service"
	serviceVersion = "0.0.2"
)

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
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a validator and committer service.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(initCmd())
	cmd.AddCommand(startCmd())
	cmd.AddCommand(clearCmd())
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
			vcConfig := vcservice.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			vc, err := vcservice.NewValidatorCommitterService(cmd.Context(), vcConfig)
			if err != nil {
				return err
			}
			defer vc.Close()

			go connection.RunServerMain(vcConfig.Server, func(server *grpc.Server, port int) {
				if vcConfig.Server.Endpoint.Port == 0 {
					vcConfig.Server.Endpoint.Port = port
				}
				protovcservice.RegisterValidationAndCommitServiceServer(server, vc)
			})

			return cobracmd.WaitUntilServiceDone(cmd.Context())
		},
	}
	cobracmd.SetDefaultFlags(cmd, serviceName, &configPath)
	return cmd
}

func initCmd() *cobra.Command {
	var (
		configPath string
		namespaces []int
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Init the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			vcConfig := vcservice.ReadConfig()

			cmd.Printf("Initializing database: %v\n", namespaces)

			return vcservice.InitDatabase(vcConfig.Database, namespaces)
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.Flags().IntSliceVar(&namespaces, "namespaces", []int{0, 1, 2, 3}, "set the namespaces to initialize")
	return cmd
}

func clearCmd() *cobra.Command {
	var (
		configPath string
		namespaces []int
	)
	cmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the database",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			vcConfig := vcservice.ReadConfig()

			cmd.Printf("Clearing database: %v\n", namespaces)

			return vcservice.ClearDatabase(vcConfig.Database, namespaces)
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.Flags().IntSliceVar(&namespaces, "namespaces", []int{0, 1, 2, 3}, "set the namespaces to clear")
	return cmd
}
