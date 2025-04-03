package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

const config = `
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

func TestEndpoints(t *testing.T) {
	t.Parallel()
	require.NoError(t, LoadYamlConfigs(config))
	conf := new(struct {
		Endpoint                     connection.Endpoint        `mapstructure:"endpoint"`
		OrdererEndpoint              connection.OrdererEndpoint `mapstructure:"orderer-endpoint"`
		JSONOrdererEndpoint          connection.OrdererEndpoint `mapstructure:"json-orderer-endpoint"`
		MultilineJSONOrdererEndpoint connection.OrdererEndpoint `mapstructure:"multiline-json-orderer-endpoint"`
		YamlJSONOrdererEndpoint      connection.OrdererEndpoint `mapstructure:"yaml-orderer-endpoint"`
	})
	Unmarshal(conf)
	expected := connection.OrdererEndpoint{
		ID:    5,
		MspID: "org",
		API:   []string{"broadcast", "deliver"},
		Endpoint: connection.Endpoint{
			Host: "localhost",
			Port: 5050,
		},
	}
	require.Equal(t, expected.Endpoint, conf.Endpoint)
	require.Equal(t, expected, conf.OrdererEndpoint)
	require.Equal(t, expected, conf.JSONOrdererEndpoint)
	require.Equal(t, expected, conf.MultilineJSONOrdererEndpoint)
	require.Equal(t, expected, conf.YamlJSONOrdererEndpoint)
}
