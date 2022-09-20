package command

import "github.com/spf13/cobra"

var (
	rootCmd = &cobra.Command{
		Use:   "blockgen",
		Short: "A generator for benchmark workloads",
		Long:  ``,
	}
)

func Execute() error {
	return rootCmd.Execute()
}
