package main

import (
	"fmt"
	"os"

	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
)

const (
	serviceName    = "mockorderingservice"
	serviceVersion = "0.0.2"
)

func main() {
	cmd := mockorderingserviceCmd()

	// On failure, Cobra prints the usage message and error string, so we only
	// need to exit with a non-0 status
	if cmd.Execute() != nil {
		os.Exit(1)
	}
}

func mockorderingserviceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   serviceName,
		Short: fmt.Sprintf("%v is a mock ordering service.", serviceName),
	}

	cmd.AddCommand(cobracmd.VersionCmd(serviceName, serviceVersion))
	cmd.AddCommand(startCmd())
	return cmd
}

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: fmt.Sprintf("Starts a %v.", serviceName),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := cobracmd.ReadYaml(configPath); err != nil {
				return err
			}
			cmd.SilenceUsage = true
			wrapper := new(struct {
				Config mock.OrdererConfig `mapstructure:"mock-ordering-service"`
			})
			config.Unmarshal(wrapper)
			conf := &wrapper.Config

			service, err := mock.NewMultiOrderer(conf)
			if err != nil {
				return err
			}
			if conf.NumService != len(conf.ServerConfigs) {
				return fmt.Errorf("number of service does not match number of server configs")
			}

			cmd.Printf("Starting %v service\n", serviceName)
			g, gCtx := errgroup.WithContext(cmd.Context())
			for i, inst := range service.Instances() {
				c := conf.ServerConfigs[i]
				inst := inst
				g.Go(func() error {
					return connection.StartService(gCtx, service, c, func(s *grpc.Server) {
						ab.RegisterAtomicBroadcastServer(s, inst)
					})
				})
			}
			return g.Wait()
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
