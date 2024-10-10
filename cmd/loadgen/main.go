package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
)

const (
	serviceName    = "blockgen"
	serviceVersion = "0.0.2"
)

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
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a block generator for coordinator service.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Starts a %v", serviceName),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			clientConfig := loadgen.ReadConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			loadBundle, err := loadgen.Starter(clientConfig)
			if err != nil {
				return err
			}
			blkStream, nsGen, client := loadBundle.BlkStream, loadBundle.NamespaceGen, loadBundle.Client

			if err = client.Start(blkStream, nsGen); err != nil {
				return err
			}

			<-client.Context().Done()
			if errors.Is(client.Context().Err(), context.DeadlineExceeded) {
				return nil
			}
			if err = context.Cause(client.Context()); !errors.Is(err, loadgen.ErrStoppedByUser) {
				return err
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	return cmd
}
