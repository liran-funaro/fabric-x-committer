package main

import (
	"fmt"
	"os"

	"github.com/hyperledger/fabric-protos-go-apiv2/peer"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthgrpc "google.golang.org/grpc/health/grpc_health_v1"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/sidecar"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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
			conf := readConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			service, err := sidecar.New(&conf)
			if err != nil {
				return err
			}
			defer service.Close()

			// enable health check server
			healthcheck := health.NewServer()
			healthcheck.SetServingStatus("", healthgrpc.HealthCheckResponse_SERVING)

			return connection.StartService(cmd.Context(), service, conf.Server, func(server *grpc.Server) {
				peer.RegisterDeliverServer(server, service.GetLedgerService())
				healthgrpc.RegisterHealthServer(server, healthcheck)
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

// readConfig reads the config.
func readConfig() sidecar.Config {
	setDefaults()
	wrapper := new(struct {
		Config sidecar.Config `mapstructure:"sidecar"`
	})
	config.Unmarshal(wrapper)
	return wrapper.Config
}

func setDefaults() {
	viper.SetDefault("sidecar.server.endpoint", "localhost:8832")
	viper.SetDefault("sidecar.monitoring.server.endpoint", "localhost:2112")

	viper.SetDefault("sidecar.orderer.channel-id", "mychannel")
	viper.SetDefault("sidecar.orderer.endpoint", "localhost:7050")

	viper.SetDefault("sidecar.committer.endpoint", "localhost:5002")
	viper.SetDefault("sidecar.committer.output-channel-capacity", 20)

	viper.SetDefault("sidecar.ledger.path", "./ledger/")
}
