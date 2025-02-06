package sidecar

import (
	"errors"

	"github.com/hyperledger/fabric-lib-go/bccsp/factory"
	cb "github.com/hyperledger/fabric-protos-go-apiv2/common"
	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/fabricx-config/common/channelconfig"
	"github.ibm.com/decentralized-trust-research/fabricx-config/internaltools/configtxgen"
	"github.ibm.com/decentralized-trust-research/fabricx-config/protoutil"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protoblocktx"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/protosigverifierservice"
	"github.ibm.com/decentralized-trust-research/scalable-committer/api/types"
	"github.ibm.com/decentralized-trust-research/scalable-committer/broadcastdeliver"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"google.golang.org/protobuf/proto"
)

// Config holds the configuration of the sidecar service. This includes
// sidecar endpoint, orderer endpoint from which the sidecar pulls the block,
// committer endpoint to which the sidecar pushes the block and pulls statuses,
// and the config of ledger service.
type Config struct {
	Monitoring *monitoring.Config       `mapstructure:"monitoring"`
	Server     *connection.ServerConfig `mapstructure:"server"`
	Orderer    broadcastdeliver.Config  `mapstructure:"orderer"`
	Committer  CoordinatorConfig        `mapstructure:"committer"`
	Ledger     LedgerConfig             `mapstructure:"ledger"`
	// ConfigBlockPath if set, it will overwrite the above configurations with the ones from the config block.
	ConfigBlockPath string `mapstructure:"config-block-path"`
	// Policies are used internally, but cannot be passed via the yaml file.
	// It will be removed once the coordinator process config TXs.
	Policies *protosigverifierservice.Policies
}

// CoordinatorConfig holds the endpoint of the coordinator component in the
// committer service.
type CoordinatorConfig struct {
	Endpoint connection.Endpoint `mapstructure:"endpoint"`
}

type LedgerConfig struct {
	Path string `mapstructure:"path"`
}

// ReadConfig reads the config.
func ReadConfig() Config {
	wrapper := new(struct {
		Config Config `mapstructure:"sidecar"`
	})
	config.Unmarshal(wrapper)
	if len(wrapper.Config.ConfigBlockPath) > 0 {
		configBlock, err := configtxgen.ReadBlock(wrapper.Config.ConfigBlockPath)
		utils.Must(err)
		err = OverwriteConfigFromBlock(&wrapper.Config, configBlock)
		utils.Must(err)
	}
	return wrapper.Config
}

// OverwriteConfigFromBlock overwrites the sidecar configuration with relevant fields from the config block.
// For now, it fetches the following:
// - Policies.
// - Orderer endpoints.
func OverwriteConfigFromBlock(conf *Config, configBlock *cb.Block) error {
	bundle, err := bundleFromConfigBlock(configBlock)
	if err != nil {
		return err
	}
	conf.Policies, err = policiesFromConfigBlock(bundle)
	if err != nil {
		return err
	}
	conf.Orderer.Endpoints, err = getDeliveryEndpointsFromConfig(bundle)
	if err != nil {
		return err
	}
	return nil
}

func bundleFromConfigBlock(configBlock *cb.Block) (*channelconfig.Bundle, error) {
	envelope, err := protoutil.ExtractEnvelope(configBlock, 0)
	if err != nil {
		return nil, err
	}
	return channelconfig.NewBundleFromEnvelope(envelope, factory.GetDefault())
}

// policiesFromConfigBlock will be removed once the coordinator will process config TXs.
func policiesFromConfigBlock(bundle *channelconfig.Bundle) (*protosigverifierservice.Policies, error) {
	ac, ok := bundle.ApplicationConfig()
	if !ok {
		return nil, errors.New("application configuration is missing")
	}
	acx, ok := ac.(*channelconfig.ApplicationConfig)
	if !ok {
		return nil, errors.New("application configuration of incorrect type")
	}
	key := acx.MetaNamespaceVerificationKey()
	p := &protoblocktx.NamespacePolicy{
		// We use existing proto here to avoid introducing new once.
		// So we encode the key schema as the identifier.
		// This will be replaced in the future with a generic policy mechanism.
		Scheme:    key.KeyIdentifier,
		PublicKey: key.KeyMaterial,
	}
	pBytes, err := proto.Marshal(p)
	if err != nil {
		return nil, err
	}
	return &protosigverifierservice.Policies{
		Policies: []*protosigverifierservice.PolicyItem{{
			Namespace: types.MetaNamespaceID,
			Policy:    pBytes,
		}},
	}, nil
}

func getDeliveryEndpointsFromConfig(bundle *channelconfig.Bundle) ([]*connection.OrdererEndpoint, error) {
	oc, ok := bundle.OrdererConfig()
	if !ok {
		return nil, errors.New("could not find orderer config")
	}

	var endpoints []*connection.OrdererEndpoint
	for orgID, org := range oc.Organizations() {
		endpointsStr := org.Endpoints()
		for _, eStr := range endpointsStr {
			e, err := connection.ParseOrdererEndpoint(eStr)
			if err != nil {
				return nil, err
			}
			e.MspID = orgID
			endpoints = append(endpoints, e)
		}
	}
	return endpoints, nil
}

func init() {
	viper.SetDefault("sidecar.server.endpoint", ":8832")
	viper.SetDefault("sidecar.metrics.endpoint", ":2112")

	viper.SetDefault("sidecar.orderer.channel-id", "mychannel")
	viper.SetDefault("sidecar.orderer.endpoint", ":7050")

	viper.SetDefault("sidecar.committer.endpoint", ":5002")
	viper.SetDefault("sidecar.committer.output-channel-capacity", 20)

	viper.SetDefault("sidecar.ledger.path", "./ledger/")
}
