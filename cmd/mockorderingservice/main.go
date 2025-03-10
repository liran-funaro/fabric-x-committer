package main

import (
	"errors"
	"fmt"
	"os"

	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
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

			service, err := mock.NewMockOrderer(conf)
			if err != nil {
				return err
			}
			if len(conf.ServerConfigs) == 0 {
				return errors.New("missing server configuration")
			}

			cmd.Printf("Starting %v service\n", serviceName)
			g, gCtx := errgroup.WithContext(cmd.Context())

			// We run the main worker, and start GRPC servers.
			g.Go(func() error {
				return connection.StartService(gCtx, service, nil, nil)
			})
			for _, subServer := range conf.ServerConfigs {
				subServer := subServer
				g.Go(func() error {
					return connection.RunGrpcServerMainWithError(gCtx, subServer, func(s *grpc.Server) {
						ab.RegisterAtomicBroadcastServer(s, service)
					})
				})
			}
			return g.Wait()
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
