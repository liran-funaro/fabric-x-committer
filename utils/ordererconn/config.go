/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package ordererconn

import (
	"github.com/cockroachdb/errors"
	"github.com/hyperledger/fabric-lib-go/bccsp/factory"
	commontypes "github.com/hyperledger/fabric-x-common/api/types"

	"github.com/hyperledger/fabric-x-committer/utils/connection"
)

type (
	// Config for the orderer-client.
	Config struct {
		Connection          ConnectionConfig `mapstructure:"connection"`
		FaultToleranceLevel string           `mapstructure:"fault-tolerance-level"`
		ChannelID           string           `mapstructure:"channel-id"`
		Identity            *IdentityConfig  `mapstructure:"identity"`
	}

	// ConnectionConfig contains the endpoints, CAs, and retry profile.
	ConnectionConfig struct {
		Endpoints []*commontypes.OrdererEndpoint `mapstructure:"endpoints"`
		TLS       connection.TLSConfig           `mapstructure:"tls"`
		Retry     *connection.RetryProfile       `mapstructure:"reconnect"`
	}

	// IdentityConfig defines the orderer's MSP.
	IdentityConfig struct {
		// MspID indicates to which MSP this client belongs to.
		MspID  string               `mapstructure:"msp-id" yaml:"msp-id"`
		MSPDir string               `mapstructure:"msp-dir" yaml:"msp-dir"`
		BCCSP  *factory.FactoryOpts `mapstructure:"bccsp" yaml:"bccsp"`
	}
)

// Fault tolerance levels.
// BFT (byzantine fault tolerance):
//   - For delivery: verifies blocks and monitors for block withholding.
//   - For broadcast: submit transactions to multiple orderers and waits for acknowledgments from a quorum.
//
// CFT (crash fault tolerance):
//   - For delivery: verifies blocks.
//   - For broadcast: submits transactions to a single orderer.
//
// NoFT (no fault tolerance):
//   - For delivery: does not verify blocks nor monitor for block withholding.
//   - For broadcast: submits transactions to a single orderer.
//   - Not for production use.
//
// UnspecifiedFT (empty string) defaults to the DefaultFT, which is the highest fault tolerance level (BFT).
const (
	UnspecifiedFT = ""
	BFT           = "BFT"
	CFT           = "CFT"
	NoFT          = "NO"
	DefaultFT     = BFT
)

const (
	// Broadcast support by endpoint.
	Broadcast = "broadcast"
	// Deliver support by endpoint.
	Deliver = "deliver"
)

// ErrEmptyEndpoint will be returned when an endpoint is empty.
var (
	ErrEmptyEndpoint = errors.New("empty endpoint")
	ErrNoEndpoints   = errors.New("no endpoints")
)

// ValidateConfig validate the configuration.
func ValidateConfig(c *Config) error {
	switch c.FaultToleranceLevel {
	case BFT, CFT, NoFT:
		// valid
	case UnspecifiedFT:
		c.FaultToleranceLevel = DefaultFT
	default:
		return errors.Newf("invalid fault tolerance level: '%s'", c.FaultToleranceLevel)
	}
	return ValidateEndpoints(c.Connection.Endpoints)
}

// ValidateEndpoints validate the configuration.
func ValidateEndpoints(ep []*commontypes.OrdererEndpoint) error {
	if len(ep) == 0 {
		return ErrNoEndpoints
	}
	uniqueEndpoints := make(map[string]string, len(ep))
	for _, e := range ep {
		if e.Host == "" || e.Port == 0 {
			return ErrEmptyEndpoint
		}
		target := e.Address()
		if other, ok := uniqueEndpoints[target]; ok {
			return errors.Newf("endpoint [%s] specified multiple times: %s, %s", target, other, e.String())
		}
		uniqueEndpoints[target] = e.String()
	}
	return nil
}
