package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen/adapters"
)

const (
	serviceName    = "loadgen"
	serviceVersion = "0.0.2"
)

func main() {
	cmd := loadgenCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func loadgenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a load generator for FabricX committer services.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	var configPath string
	var onlyNamespace bool
	var onlyWorkload bool
	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Starts a %v", serviceName),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			conf := readConfig()
			cmd.Printf("Starting %v service\n", serviceName)

			if onlyNamespace {
				conf.Generate = adapters.Phases{Namespaces: true}
			}
			if onlyWorkload {
				conf.Generate = adapters.Phases{Load: true}
			}

			client, err := loadgen.NewLoadGenClient(conf)
			if err != nil {
				return err
			}
			return client.Run(cmd.Context())
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.PersistentFlags().BoolVar(&onlyNamespace, "only-namespace", false, "only run namespace generation")
	cmd.PersistentFlags().BoolVar(&onlyWorkload, "only-workload", false, "only run workload generation")
	return cmd
}

// readConfig is a function that reads the client configuration.
func readConfig() *loadgen.ClientConfig {
	wrapper := new(loadgen.ClientConfig)
	config.Unmarshal(wrapper)
	return wrapper
}
