package main

import (
	"fmt"
	"os"
	"time"

	ab "github.com/hyperledger/fabric-protos-go-apiv2/orderer"
	"github.com/spf13/cobra"
	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/cobracmd"
	"github.ibm.com/decentralized-trust-research/scalable-committer/mock"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"google.golang.org/grpc"
)

const (
	serviceName    = "mockorderingservice"
	serviceVersion = "0.0.2"
)

// MockOrderingServiceConfig holds the server configuration of
// the mock ordering service.
type MockOrderingServiceConfig struct {
	Server       *connection.ServerConfig `mapstructure:"server"`
	BlockSize    uint64                   `mapstructure:"block-size"`
	BlockTimeout time.Duration            `mapstructure:"block-timeout"`
}

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
				Config MockOrderingServiceConfig `mapstructure:"mock-ordering-service"`
			})
			config.Unmarshal(wrapper)
			conf := &wrapper.Config
			cmd.Printf("Starting %v service\n", serviceName)

			services := mock.NewMockOrderingServices(1, conf.BlockSize, conf.BlockTimeout)
			defer func() {
				for _, service := range services {
					service.Close()
				}
			}()
			return connection.RunGrpcServerMainWithError(cmd.Context(), conf.Server, func(server *grpc.Server) {
				ab.RegisterAtomicBroadcastServer(server, services[0])
			})
		},
	}

	cmd.PersistentFlags().StringVar(&configPath, "configs", "", "set the absolute path to the config file")
	return cmd
}
