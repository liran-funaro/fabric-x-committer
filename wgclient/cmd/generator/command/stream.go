package command

import (
	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

var (
	streamCmd = &cobra.Command{
		Use:   "stream",
		Short: "A stream generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			config := workload.ReadConfig()
			if profilePath != "" {
				config.Generator.Profile = profilePath
			}
			if host != "" {
				config.Endpoint = *connection.CreateEndpoint(host)
			}
			if prometheusEndpoint != "" {
				config.Prometheus.Endpoint = *connection.CreateEndpoint(prometheusEndpoint)
			}
			if latencyEndpoint != "" {
				config.Prometheus.LatencyEndpoint = *connection.CreateEndpoint(latencyEndpoint)
			}

			client.GenerateAndPump(config)
		},
	}
)

func init() {
	rootCmd.AddCommand(streamCmd)

	streamCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile (required)")
	streamCmd.Flags().StringVarP(&host, "host", "", "localhost:5002", "coordinator host addr")
	streamCmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", ":2112", "path to prometheus metrics")
	streamCmd.Flags().StringVar(&latencyEndpoint, "prometheus-latency-endpoint", ":14268", "path to prometheus metrics")
}
