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
			client.GenerateAndPump(profilePath, host)
		},
	}
)

func init() {
	rootCmd.AddCommand(streamCmd)

	streamCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile (required)")
	streamCmd.MarkFlagRequired("profile")

	streamCmd.Flags().StringVarP(&host, "host", "", "localhost:5002", "coordinator host addr")
	streamCmd.MarkFlagRequired("host")

}
