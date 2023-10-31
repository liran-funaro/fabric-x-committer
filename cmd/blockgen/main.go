package main

import (
	"fmt"
	"log"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
)

var configPath string

func main() {
	cmd := blockgenCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func blockgenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blockgen",
		Short: "blockgen is a block generator for coordinator service.",
	}
	cmd.AddCommand(versionCmd())
	cmd.AddCommand(startCmd())
	return cmd
}

func versionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the version of the blockgen.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("trailing arguments detected")
			}

			cmd.SilenceUsage = true
			cmd.Println("blockgen 0.2")

			return nil
		},
	}

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Starts a blockgen",
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := readConfig(configPath)
			if err != nil {
				return errors.Wrap(err, "failed to read config")
			}
			if _, blockGen, client, err := BlockgenStarter(c); err != nil {
				return err
			} else {
				return client.Start(blockGen)
			}
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	return cmd
}

func BlockgenStarter(c *ClientConfig) (*perfMetrics, *loadgen.BlockStreamGenerator, blockGenClient, error) {
	client, metrics, err := createClient(c)
	if err != nil {
		return nil, nil, nil, errors.Wrap(err, "failed creating client")
	}

	promErrChan := metrics.provider.StartPrometheusServer()

	go func() {
		if errProm := <-promErrChan; errProm != nil {
			log.Panic(err) // nolint: revive
		}
	}()

	blockGen := loadgen.StartBlockGenerator(c.LoadProfile, c.RateLimit)

	return metrics, blockGen, client, nil
}
