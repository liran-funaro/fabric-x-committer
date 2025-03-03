package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
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
			conf := readConfig()
			cmd.Printf("Starting %v service\n", serviceName)
			vc, err := vcservice.NewValidatorCommitterService(cmd.Context(), conf)
			if err != nil {
				return err
			}
			defer vc.Close()
			return connection.StartService(cmd.Context(), vc, conf.Server, func(server *grpc.Server) {
				protovcservice.RegisterValidationAndCommitServiceServer(server, vc)
			})
		},
	}
	cobracmd.SetDefaultFlags(cmd, serviceName, &configPath)
	return cmd
}

func clearCmd() *cobra.Command {
	var (
		configPath string
		namespaces []string
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
			vcConfig := readConfig()

			cmd.Printf("Clearing database: %v\n", namespaces)

			return vcservice.ClearDatabase(cmd.Context(), vcConfig.Database, namespaces)
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.Flags().StringSliceVar(&namespaces, "namespaces", []string{}, "set the namespaces to clear")
	return cmd
}

// readConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func readConfig() *vcservice.ValidatorCommitterServiceConfig {
	setDefaults()

	wrapper := new(struct {
		Config vcservice.ValidatorCommitterServiceConfig `mapstructure:"validator-committer-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	// defaults for ServerConfig
	prefix := "validator-committer-service.server.endpoint."
	viper.SetDefault("validator-committer-service.server.endpoint", "localhost:6001")

	// defaults for DatabaseConfig
	prefix = "validator-committer-service.database."
	viper.SetDefault(prefix+"host", "localhost")
	viper.SetDefault(prefix+"port", 5433)
	viper.SetDefault(prefix+"username", "yugabyte")
	viper.SetDefault(prefix+"password", "yugabyte")
	viper.SetDefault(prefix+"database", "yugabyte")
	viper.SetDefault(prefix+"max-connections", 20)
	viper.SetDefault(prefix+"min-connections", 10)
	viper.SetDefault(prefix+"retry.max-elapsed-time", 20*time.Second)

	// defaults for ResourceLimitsConfig
	prefix = "validator-committer-service.resource-limits."
	viper.SetDefault(prefix+"max-workers-for-preparer", 1)
	viper.SetDefault(prefix+"max-workers-for-validator", 1)
	viper.SetDefault(prefix+"max-workers-for-committer", 20)
	viper.SetDefault(prefix+"min-transaction-batch-size", 1)
	viper.SetDefault(prefix+"timeout-for-min-transaction-batch-size", 5*time.Second)

	// defaults for monitoring.config
	viper.SetDefault("validator-committer-service.monitoring.server.endpoint", "localhost:6002")
}
