package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/verifier"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

const (
	serviceName    = "sig-verification"
	serviceVersion = "0.0.1"
)

func main() {
	cmd := sigverifierCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func sigverifierCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a service that verifies the transaction's signatures.", serviceName),
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
			conf := readConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			service := verifier.New(conf)
			return connection.StartService(cmd.Context(), service, conf.Server, func(server *grpc.Server) {
				protosigverifierservice.RegisterVerifierServer(server, service)
			})
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
		"parallelism",
		"sets the value of the parallelism in the config file",
		fmt.Sprintf("%v.parallel-executor.parallelism", serviceName),
	)

	cobracmd.CobraInt(
		cmd,
		"batch-size-cutoff",
		"Batch time cutoff limit",
		fmt.Sprintf("%v.parallel-executor.batch-size-cutoff", serviceName),
	)

	cobracmd.CobraInt(
		cmd,
		"channel-buffer-size",
		"Channel buffer size for the executor",
		fmt.Sprintf("%v.parallel-executor.channel-buffer-size", serviceName),
	)

	cobracmd.CobraString(
		cmd,
		"scheme",
		"Verification scheme",
		fmt.Sprintf("%v.scheme", serviceName),
	)

	cobracmd.CobraDuration(
		cmd,
		"batch-time-cutoff",
		"Batch time cutoff limit",
		fmt.Sprintf("%v.parallel-executor.batch-time-cutoff", serviceName),
	)
}

func readConfig() *verifier.Config {
	setDefaults()
	wrapper := new(struct {
		Config verifier.Config `mapstructure:"sig-verification"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	viper.SetDefault("sig-verification.server.endpoint", "localhost:5000")
	viper.SetDefault("sig-verification.monitoring.server.endpoint", "localhost:2112")

	viper.SetDefault("sig-verification.scheme", "ECDSA")

	viper.SetDefault("sig-verification.parallel-executor.parallelism", 4)
	viper.SetDefault("sig-verification.parallel-executor.batch-time-cutoff", "500ms")
	viper.SetDefault("sig-verification.parallel-executor.batch-size-cutoff", 50)
	viper.SetDefault("sig-verification.parallel-executor.channel-buffer-size", 50)

	viper.SetDefault("logging.development", "false")
	viper.SetDefault("logging.enabled", "true")
	viper.SetDefault("logging.level", "Info")
}
