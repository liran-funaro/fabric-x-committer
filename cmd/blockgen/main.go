package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/loadgen"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

var (
	configPath     string
	component      string
	metrics        *perfMetrics
	stopSender     chan any
	latencyTracker *sync.Map
	blockSize      int
)

// BlockgenConfig is the configuration for blockgen.
type BlockgenConfig struct {
	CoordinatorEndpoint     *connection.Endpoint   `mapstructure:"coordinator-endpoint"`
	VCServiceEndpoints      []*connection.Endpoint `mapstructure:"vcservice-endpoints"`
	Monitoring              *monitoring.Config     `mapstructure:"monitoring"`
	RateLimit               int                    `mapstructure:"rate-limit"`
	LatencySamplingInterval time.Duration          `mapstructure:"latency-sampling-interval"`
}

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
			if configPath == "" {
				return errors.New("--configs flag must be set to the path of configuration file")
			}

			if component == "" {
				return errors.New("--component flag must be set to the component name for which load is generated")
			}

			c, err := readConfig()
			if err != nil {
				return err
			}

			metrics = newBlockgenServiceMetrics(c.Monitoring.Metrics.Enable)
			var promErrChan <-chan error
			if metrics.enabled {
				promErrChan = metrics.provider.StartPrometheusServer(c.Monitoring.Metrics.Endpoint)
			}

			go func() {
				if errProm := <-promErrChan; errProm != nil {
					log.Panic(err) // nolint: revive
				}
			}()

			profile := loadgen.LoadProfileFromYaml(configPath)
			blockSize = int(profile.Block.Size)
			blockGen := loadgen.StartBlockGenerator(profile)
			latencyTracker = &sync.Map{}

			switch component {
			case "coordinator":
				stopSender = make(chan any)
				err = generateLoadForCoordinatorService(cmd, c, blockGen)
			case "vcservice":
				stopSender = make(chan any, len(c.VCServiceEndpoints))
				err = generateLoadForVCService(cmd, c, blockGen)
			default:
				err = fmt.Errorf("invalid component name: %s", component)
			}

			return err
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path of config directory")
	cmd.PersistentFlags().StringVar(&component, "component", "", "set the component name for which load is generated")
	return cmd
}

func readConfig() (*BlockgenConfig, error) {
	if err := config.ReadYamlConfigs([]string{configPath}); err != nil {
		return nil, err
	}
	wrapper := new(struct {
		Config BlockgenConfig `mapstructure:"blockgen"`
	})
	config.Unmarshal(wrapper)

	if wrapper.Config.RateLimit == 0 {
		return nil, errors.New("rate-limit must be set")
	}

	if wrapper.Config.LatencySamplingInterval == 0 {
		return nil, errors.New("latency-sampling-interval must be set")
	}

	return &wrapper.Config, nil
}
