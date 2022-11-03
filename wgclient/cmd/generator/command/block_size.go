package command

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

var (
	sampleSize   int
	blockSizeCmd = &cobra.Command{
		Use:   "block-size",
		Short: "Reports the average size of the serialized block",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			result := client.GetBlockSize(profilePath, sampleSize)
			fmt.Printf("Average block size for profile '%s' (on %d samples): %.2f KB", profilePath, sampleSize, result/1024)
		},
	}
)

func init() {
	rootCmd.AddCommand(blockSizeCmd)

	blockSizeCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile (required)")
	blockSizeCmd.MarkFlagRequired("profile")

	blockSizeCmd.Flags().IntVarP(&sampleSize, "samples", "", 10, "block samples to create for the average")
}
