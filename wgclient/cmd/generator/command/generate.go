package command

import (
	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"
)

var (
	profilePath        string
	outputPath         string
	prometheusEnabled  bool
	prometheusEndpoint string
	genCmd             = &cobra.Command{
		Use:   "generate",
		Short: "A generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			workload.Generate(profilePath, outputPath)
		},
	}
)

func init() {
	rootCmd.AddCommand(genCmd)

	genCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile (required)")
	genCmd.MarkFlagRequired("profile")

	genCmd.Flags().StringVarP(&outputPath, "out", "o", "", "path to block output file (required)")
	genCmd.MarkFlagRequired("out")

	genCmd.Flags().BoolVar(&prometheusEnabled, "prometheus-enabled", true, "enable prometheus metrics")
	genCmd.Flags().StringVar(&prometheusEndpoint, "prometheus-endpoint", ":2112", "path to prometheus metrics")
}
