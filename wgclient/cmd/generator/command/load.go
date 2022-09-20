package command

import (
	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

var (
	blockFile string
	loadCmd   = &cobra.Command{
		Use:   "load",
		Short: "A generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			client.ReadAndForget(blockFile)
		},
	}
)

func init() {
	rootCmd.AddCommand(loadCmd)

	loadCmd.Flags().StringVarP(&blockFile, "in", "", "", "path to block file (required)")
	loadCmd.MarkFlagRequired("in")
}
