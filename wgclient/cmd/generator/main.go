package main

import (
	"fmt"
	"os"

	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload"

	"github.com/spf13/cobra"
)

var (
	profilePath string
	outputPath  string
	rootCmd     = &cobra.Command{
		Use:   "blockgen",
		Short: "A generator for benchmark workloads",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			workload.Generate(profilePath, outputPath)
		},
	}
)

func init() {
	rootCmd.Flags().StringVarP(&profilePath, "profile", "p", "", "path to workload profile (required)")
	rootCmd.MarkFlagRequired("profile")

	rootCmd.Flags().StringVarP(&outputPath, "out", "o", "", "path to block output file (required)")
	rootCmd.MarkFlagRequired("out")
}

func Execute() error {
	return rootCmd.Execute()
}

func main() {
	if err := Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
