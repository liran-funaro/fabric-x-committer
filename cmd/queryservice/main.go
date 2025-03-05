package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoqueryservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
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
			conf := readConfig()
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

// readConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func readConfig() *queryservice.Config {
	setDefaults()

	wrapper := new(struct {
		Config queryservice.Config `mapstructure:"query-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	// defaults for server config.
	viper.SetDefault("query-service.server.endpoint", "localhost:7003")

	// defaults for database config,
	prefix := "query-service.database."
	viper.SetDefault(prefix+"endpoints", []*connection.Endpoint{
		{Host: "localhost", Port: 5433},
	})
	viper.SetDefault(prefix+"username", "yugabyte")
	viper.SetDefault(prefix+"password", "yugabyte")
	viper.SetDefault(prefix+"database", "yugabyte")
	viper.SetDefault(prefix+"max-connections", 20)
	viper.SetDefault(prefix+"min-connections", 10)

	// defaults for monitoring config.
	viper.SetDefault("query-service.monitoring.server.endpoint", "localhost:7004")

	// defaults for batching.
	prefix = "query-service."
	viper.SetDefault(prefix+"min-batch-keys", 1024)
	viper.SetDefault(prefix+"max-batch-wait", 100*time.Millisecond)
	viper.SetDefault(prefix+"view-aggregation-window", 100*time.Millisecond)
	viper.SetDefault(prefix+"max-aggregated-views", 1024)
	viper.SetDefault(prefix+"max-view-timeout", 10*time.Second)
}
