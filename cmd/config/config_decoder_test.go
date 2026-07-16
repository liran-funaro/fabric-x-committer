/*
Copyright IBM Corp. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package config

import (
	"bytes"
	"strings"
	"testing"

	commontypes "github.com/hyperledger/fabric-x-common/api/types"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"

	"github.com/hyperledger/fabric-x-committer/loadgen/workload"
	"github.com/hyperledger/fabric-x-committer/utils/connection"
	"github.com/hyperledger/fabric-x-committer/utils/serve"
)

const config = `
server: localhost:5050
endpoint: localhost:5050
orderer-endpoint: id=5,msp-id=org,broadcast,deliver,localhost:5050
json-orderer-endpoint: {"id":5,"msp-id":"org","api":["broadcast","deliver"],"host":"localhost","port":5050}
multiline-json-orderer-endpoint: >
    {
        "id": 5,
        "msp-id": "org",
        "api": ["broadcast","deliver"],
        "host": "localhost",
        "port": 5050
    }
yaml-orderer-endpoint:
    id: 5
    msp-id: org
    api:
        - broadcast
        - deliver
    host: localhost
    port: 5050
`

// distributionConfig mirrors how a *workload.Distribution field is embedded in
// the real load-generator configuration (e.g., TransactionProfile.ReadWriteCount).
type distributionConfig struct {
	Count *workload.Distribution `mapstructure:"count"`
}

// TestDistributionShorthand verifies that a bare number is decoded as a const
// distribution, while the full distribution forms keep working unchanged.
func TestDistributionShorthand(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		yaml     string
		expected *workload.Distribution
	}{
		{
			name:     "integer shorthand",
			yaml:     "count: 2",
			expected: workload.NewConstantDistribution(2),
		},
		{
			name:     "float shorthand",
			yaml:     "count: 2.5",
			expected: workload.NewConstantDistribution(2.5),
		},
		{
			name:     "negative shorthand",
			yaml:     "count: -1",
			expected: workload.NewConstantDistribution(-1),
		},
		{
			name:     "zero shorthand",
			yaml:     "count: 0",
			expected: workload.NewConstantDistribution(0),
		},
		{
			name:     "explicit const form still works",
			yaml:     "count:\n  const: 2",
			expected: workload.NewConstantDistribution(2),
		},
		{
			name:     "uniform form still works",
			yaml:     "count:\n  uniform:\n    min: 0\n    max: 1",
			expected: workload.NewUniformDistribution(0, 1),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := viper.New()
			require.NoError(t, readYamlConfigsFromIO(v, bytes.NewBufferString(tc.yaml)))
			conf := new(distributionConfig)
			require.NoError(t, unmarshal(v, conf))
			require.Equal(t, tc.expected, conf.Count)
		})
	}
}

// TestDistributionShorthandRejectsNonNumeric ensures a non-numeric scalar is not
// silently coerced into a const distribution; it surfaces a decode error instead.
func TestDistributionShorthandRejectsNonNumeric(t *testing.T) {
	t.Parallel()
	v := viper.New()
	require.NoError(t, readYamlConfigsFromIO(v, bytes.NewBufferString("count: abc")))
	require.Error(t, unmarshal(v, new(distributionConfig)))
}

func TestParseEndpoint(t *testing.T) {
	t.Parallel()
	v := viper.New()
	require.NoError(t, readYamlConfigsFromIO(v, bytes.NewBufferString(config)))
	conf := new(struct {
		Server                       serve.ServerConfig          `mapstructure:"server"`
		Endpoint                     connection.Endpoint         `mapstructure:"endpoint"`
		OrdererEndpoint              commontypes.OrdererEndpoint `mapstructure:"orderer-endpoint"`
		JSONOrdererEndpoint          commontypes.OrdererEndpoint `mapstructure:"json-orderer-endpoint"`
		MultilineJSONOrdererEndpoint commontypes.OrdererEndpoint `mapstructure:"multiline-json-orderer-endpoint"`
		YamlJSONOrdererEndpoint      commontypes.OrdererEndpoint `mapstructure:"yaml-orderer-endpoint"`
	})
	require.NoError(t, unmarshal(v, conf))
	expected := commontypes.OrdererEndpoint{
		ID:    5,
		MspID: "org",
		API:   []string{"broadcast", "deliver"},
		Host:  "localhost",
		Port:  5050,
	}
	expectedEndpoint := connection.Endpoint{
		Host: "localhost",
		Port: 5050,
	}
	require.Equal(t, expectedEndpoint, conf.Server.Endpoint)
	require.Equal(t, expectedEndpoint, conf.Endpoint)
	require.Equal(t, expected, conf.OrdererEndpoint)
	require.Equal(t, expected, conf.JSONOrdererEndpoint)
	require.Equal(t, expected, conf.MultilineJSONOrdererEndpoint)
	require.Equal(t, expected, conf.YamlJSONOrdererEndpoint)
}

func TestParseEndpointEdgeCases(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		input    string
		expected *connection.Endpoint
	}{
		{
			name:     "empty string returns empty endpoint",
			input:    "",
			expected: &connection.Endpoint{},
		},
		{
			name:  "valid hostname and port",
			input: "localhost:8080",
			expected: &connection.Endpoint{
				Host: "localhost",
				Port: 8080,
			},
		},
		{
			name:  "valid IP address and port",
			input: "192.168.1.1:9090",
			expected: &connection.Endpoint{
				Host: "192.168.1.1",
				Port: 9090,
			},
		},
		{
			name:  "IPv6 address with brackets",
			input: "[::1]:8080",
			expected: &connection.Endpoint{
				Host: "::1",
				Port: 8080,
			},
		},
		{
			name:  "IPv6 full address with brackets",
			input: "[2001:db8::1]:443",
			expected: &connection.Endpoint{
				Host: "2001:db8::1",
				Port: 443,
			},
		},
		{
			name:  "hostname with hyphen",
			input: "my-service:3000",
			expected: &connection.Endpoint{
				Host: "my-service",
				Port: 3000,
			},
		},
		{
			name:  "FQDN with port",
			input: "service.example.com:443",
			expected: &connection.Endpoint{
				Host: "service.example.com",
				Port: 443,
			},
		},
		{
			name:  "empty host with port",
			input: ":8080",
			expected: &connection.Endpoint{
				Host: "",
				Port: 8080,
			},
		},
		{
			name:  "port at upper boundary",
			input: "localhost:65535",
			expected: &connection.Endpoint{
				Host: "localhost",
				Port: 65535,
			},
		},
		{
			name:  "port at lower boundary",
			input: "localhost:0",
			expected: &connection.Endpoint{
				Host: "localhost",
				Port: 0,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseEndpoint(tc.input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, result)
		})
	}

	for _, tc := range []struct {
		name     string
		input    string
		errorMsg string
	}{
		{
			name:     "missing port",
			input:    "localhost",
			errorMsg: "could not split host and port",
		},
		{
			name:     "invalid port - non-numeric",
			input:    "localhost:abc",
			errorMsg: "could not convert port to integer",
		},
		{
			name:     "invalid port - empty",
			input:    "localhost:",
			errorMsg: "could not convert port to integer",
		},
		{
			name:     "invalid port - space",
			input:    "localhost: 1234",
			errorMsg: "could not convert port to integer",
		},
		{
			name:     "multiple colons without brackets",
			input:    "::1:8080",
			errorMsg: "could not split host and port",
		},
		{
			name:     "negative port",
			input:    "localhost:-1",
			errorMsg: "port must be between 0 and 65535",
		},
		{
			name:     "port too large",
			input:    "localhost:65536",
			errorMsg: "port must be between 0 and 65535",
		},
		{
			name:     "port way too large",
			input:    "localhost:99999",
			errorMsg: "port must be between 0 and 65535",
		},
		{
			name:     "invalid hostname - starts with hyphen",
			input:    "-invalid:8080",
			errorMsg: "invalid hostname",
		},
		{
			name:     "invalid hostname - ends with hyphen",
			input:    "invalid-:8080",
			errorMsg: "invalid hostname",
		},
		{
			name:     "invalid hostname - special characters",
			input:    "host_name:8080",
			errorMsg: "invalid hostname",
		},
		{
			name:     "invalid hostname - spaces",
			input:    "host name:8080",
			errorMsg: "invalid hostname",
		},
		{
			name:     "hostname too long",
			input:    strings.Repeat("a", 254) + ":8080",
			errorMsg: "hostname exceeds maximum length",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseEndpoint(tc.input)
			require.ErrorContains(t, err, tc.errorMsg)
			require.Nil(t, result)
		})
	}
}
