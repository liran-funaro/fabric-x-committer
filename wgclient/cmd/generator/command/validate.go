package command

import (
	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

var (
	validateCmd = &cobra.Command{
		Use:   "validate",
		Short: "A generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			client.Validate(blockFile)
		},
	}
)

func init() {
	rootCmd.AddCommand(validateCmd)

	validateCmd.Flags().StringVarP(&blockFile, "results", "", "", "path to results file (required)")
	validateCmd.MarkFlagRequired("results")
}
