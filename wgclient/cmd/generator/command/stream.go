package command

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"

	"github.com/spf13/cobra"
)

var (
	streamCmd = &cobra.Command{
		Use:   "stream",
		Short: "A stream generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			client.GenerateAndPump(profilePath, host, prometheusEndpoint, latencyEndpoint)
		},
	}
)

func init() {
	rootCmd.AddCommand(streamCmd)

	streamCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile (required)")
	streamCmd.MarkFlagRequired("profile")

	streamCmd.Flags().StringVarP(&host, "host", "", "localhost:5002", "coordinator host addr")
	streamCmd.MarkFlagRequired("host")

	streamCmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", ":2112", "path to prometheus metrics")
	streamCmd.Flags().StringVar(&latencyEndpoint, "prometheus-latency-endpoint", ":14268", "path to prometheus metrics")

}
