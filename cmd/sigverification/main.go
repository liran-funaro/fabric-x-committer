package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	sigverification "github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/serverconfig"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/verifierserver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/grpc"
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
			conf := serverconfig.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)
			fmt.Println(conf)

			m := monitoring.LaunchMonitoring(conf.Monitoring, &metrics.Provider{}).(*metrics.Metrics)
			service := verifierserver.New(&conf.ParallelExecutor, m)
			return connection.RunGrpcServerMainWithError(cmd.Context(), conf.Server, func(server *grpc.Server) {
				sigverification.RegisterVerifierServer(server, service)
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
