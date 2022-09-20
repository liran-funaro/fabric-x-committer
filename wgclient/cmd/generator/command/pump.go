package command

import (
	"github.com/spf13/cobra"
	"github.ibm.com/distributed-trust-research/scalable-committer/wgclient/workload/client"
)

var (
	host    string
	port    int
	pumpCmd = &cobra.Command{
		Use:   "pump",
		Short: "Pushes blocks to the coordinator via the wire",
		Long:  ``,
		Run: func(cmd *cobra.Command, args []string) {
			client.PumpToCoordinator(blockFile, host, port)
		},
	}
)

func init() {
	rootCmd.AddCommand(pumpCmd)

	pumpCmd.Flags().StringVarP(&blockFile, "in", "", "", "path to block file (required)")
	pumpCmd.MarkFlagRequired("in")

	pumpCmd.Flags().StringVarP(&host, "host", "", "localhost", "coordinator host addr")
	pumpCmd.MarkFlagRequired("host")

	pumpCmd.Flags().IntVarP(&port, "port", "", 5003, "coordinator host port")
	pumpCmd.MarkFlagRequired("port")
}
