package main

import (
	"fmt"
	"os"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protovcservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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
				return errors.Wrap(err, "failed to read config")
			}
			cmd.SilenceUsage = true
			conf := readConfig()
			cmd.Printf("Starting %v service\n", serviceName)
			service, err := vc.NewValidatorCommitterService(cmd.Context(), conf)
			if err != nil {
				return errors.Wrap(err, "failed to create validator committer service")
			}
			defer service.Close()
			return connection.StartService(cmd.Context(), service, conf.Server, func(server *grpc.Server) {
				protovcservice.RegisterValidationAndCommitServiceServer(server, service)
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
				return errors.Wrap(err, "failed to read config")
			}
			cmd.SilenceUsage = true
			vcConfig := readConfig()
			cmd.Printf("Clearing database: %v\n", namespaces)
			return vc.ClearDatabase(cmd.Context(), vcConfig.Database, namespaces)
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.Flags().StringSliceVar(&namespaces, "namespaces", []string{}, "set the namespaces to clear")
	return cmd
}

// readConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func readConfig() *vc.ValidatorCommitterServiceConfig {
	setDefaults()

	wrapper := new(struct {
		Config vc.ValidatorCommitterServiceConfig `mapstructure:"validator-committer-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	// defaults for ServerConfig
	viper.SetDefault("validator-committer-service.server.endpoint", "localhost:6001")

	// defaults for DatabaseConfig
	dbPrefix := "validator-committer-service.database."
	viper.SetDefault(dbPrefix+"endpoints", []*connection.Endpoint{
		{Host: "localhost", Port: 5433},
	})
	viper.SetDefault(dbPrefix+"username", "yugabyte")
	viper.SetDefault(dbPrefix+"password", "yugabyte")
	viper.SetDefault(dbPrefix+"database", "yugabyte")
	viper.SetDefault(dbPrefix+"max-connections", 20)
	viper.SetDefault(dbPrefix+"min-connections", 10)
	viper.SetDefault(dbPrefix+"retry.max-elapsed-time", 20*time.Second)

	// defaults for ResourceLimitsConfig
	limitPrefix := "validator-committer-service.resource-limits."
	viper.SetDefault(limitPrefix+"max-workers-for-preparer", 1)
	viper.SetDefault(limitPrefix+"max-workers-for-validator", 1)
	viper.SetDefault(limitPrefix+"max-workers-for-committer", 20)
	viper.SetDefault(limitPrefix+"min-transaction-batch-size", 1)
	viper.SetDefault(limitPrefix+"timeout-for-min-transaction-batch-size", 5*time.Second)

	// defaults for monitoring.config
	viper.SetDefault("validator-committer-service.monitoring.server.endpoint", "localhost:6002")
}
