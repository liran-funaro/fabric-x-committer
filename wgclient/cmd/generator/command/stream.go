package command

import (
	"fmt"
	"math"
	"os"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/latency"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/metrics"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload"
	"github.ibm.com/decentralized-trust-research/scalable-committer/wgclient/workload/client"
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

			pp := workload.LoadProfileFromYaml(config.Generator.Profile)
			if invalidSigs > 1 || doubleSpends > 1 {
				panic("invalid conflict params")
			} else if invalidSigs >= 0 || doubleSpends >= 0 {
				fmt.Println("Overriding conflict params.")
				pp.Conflicts = &workload.ConflictProfile{
					Scenario: nil,
					Statistical: &workload.StatisticalConflicts{
						InvalidSignatures: math.Max(invalidSigs, 0),
						DoubleSpends:      math.Max(doubleSpends, 0),
					},
				}
			}

			fmt.Println("GOGC = " + os.Getenv("GOGC"))

			client.GenerateAndPump(config, pp)
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
	streamCmd.Flags().Float64Var(&invalidSigs, "invalid-sigs", -1, "percentage of invalid signatures, between 0 and 1")
	streamCmd.Flags().Float64Var(&doubleSpends, "double-spends", -1, "percentage of double spends, between 0 and 1")
}
