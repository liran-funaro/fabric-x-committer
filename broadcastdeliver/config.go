package broadcastdeliver

import (
	"github.com/hyperledger/fabric-lib-go/bccsp/factory"
	"github.com/pkg/errors"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type (
	// Config for the orderer-client.
	Config struct {
		Endpoints         []*connection.OrdererEndpoint `mapstructure:"endpoints"`
		ConnectionProfile *OrdererConnectionProfile     `mapstructure:"connection-profile"`
		SignedEnvelopes   bool                          `mapstructure:"signed-envelopes"`
		ConsensusType     ConsensusType                 `mapstructure:"consensus-type"`
		ChannelID         string                        `mapstructure:"channel-id"`
		Retry             *connection.RetryProfile      `mapstructure:"reconnect"`
	}

	// OrdererConnectionProfile defines the orderer's MSP.
	OrdererConnectionProfile struct {
		// MspID indicates to which MSP this client belongs to.
		MspID string `mapstructure:"msp-id"`
		// RootCA The raw root CAs for the Orderers.
		RootCA [][]byte `mapstructure:"root-ca"`
		// RootCAPaths The path to the root CAs for the Orderers.
		RootCAPaths []string             `mapstructure:"root-ca-paths"`
		MSPDir      string               `mapstructure:"msp-dir"`
		BCCSP       *factory.FactoryOpts `mapstructure:"bccsp"`
	}

	// ConsensusType can be either CFT or BFT.
	ConsensusType = string
)

const (
	// Cft client support for crash fault tolerance.
	Cft ConsensusType = "CFT"
	// Bft client support for byzantine fault tolerance.
	Bft = "BFT"
	// DefaultConsensus default fault tolerance.
	DefaultConsensus = Cft

	// Broadcast support by endpoint.
	Broadcast = "broadcast"
	// Deliver support by endpoint.
	Deliver = "deliver"
)

var (
	// ErrEmptyEndpoint is returned when endpoint does not contain a port.
	ErrEmptyEndpoint = errors.New("empty endpoint")
	// ErrNoEndpoints is returned when no endpoints are supplied.
	ErrNoEndpoints = errors.New("no endpoints")
)

func validateConfig(c *Config) error {
	if c.ConsensusType == "" {
		c.ConsensusType = DefaultConsensus
	}
	if c.ConsensusType != Bft && c.ConsensusType != Cft {
		return errors.Errorf("unsupported orderer type %s", c.ConsensusType)
	}
	if len(c.Endpoints) == 0 {
		return ErrNoEndpoints
	}
	uniqueEndpoints := make(map[string]string)
	for _, e := range c.Endpoints {
		if e.Empty() {
			return ErrEmptyEndpoint
		}
		target := e.Address()
		if other, ok := uniqueEndpoints[target]; ok {
			return errors.Errorf("endpoint [%s] specified multiple times: %s, %s", target, other, e.String())
		}
		uniqueEndpoints[target] = e.String()
	}
	return nil
}
