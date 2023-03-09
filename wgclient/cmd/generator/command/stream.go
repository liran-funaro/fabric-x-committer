package command

import (
	"fmt"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/monitoring/metrics"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

var (
	configs   = make([]string, 0)
	streamCmd = &cobra.Command{
		Use:   "stream",
		Short: "A stream generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			config := workload.ReadConfig(configs)
			if profilePath != "" {
				config.Generator.Profile = profilePath
			}
			if host != "" {
				config.Endpoint = *connection.CreateEndpoint(host)
			}
			if prometheusEndpoint != "" {
				config.Monitoring.Metrics = &metrics.Config{
					Endpoint: connection.CreateEndpoint(prometheusEndpoint),
				}
			}
			if latencyEndpoint != "" {
				config.Monitoring.Latency = &latency.Config{
					Endpoint: connection.CreateEndpoint(latencyEndpoint),
				}
			}

			fmt.Println("GOGC = " + os.Getenv("GOGC"))

			client.GenerateAndPump(config)
		},
	}
)

func init() {
	rootCmd.AddCommand(streamCmd)

	streamCmd.Flags().StringSliceVarP(&configs, "configs", "c", []string{}, "config file paths")
	streamCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile")
	streamCmd.Flags().StringVarP(&host, "host", "", "", "coordinator host addr")
	streamCmd.Flags().StringVar(&prometheusEndpoint, "metrics-endpoint", "", "path to prometheus metrics")
	streamCmd.Flags().StringVar(&latencyEndpoint, "latency-endpoint", "", "path to prometheus metrics")
}
