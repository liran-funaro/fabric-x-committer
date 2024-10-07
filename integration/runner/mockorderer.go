package runner

import (
	"testing"
	"time"

	"github.com/tedsuo/ifrit"
)

type (
	// OrdererProcess represents a orderer process.
	OrdererProcess struct {
		Name    string
		Process ifrit.Process
		Config  *OrdererConfig
	}

	// OrdererConfig represents the configuration of the orderer process.
	OrdererConfig struct {
		ServerEndpoint string
		BlockSize      uint64
		BlockTimeout   time.Duration
	}
)

const (
	ordererConfigTemplate = "../configtemplates/mockorderer-config-template.yaml"
	ordererCmd            = "../../bin/mockorderingservice"
	numPortsForOrderer    = 1
)

// NewOrdererProcess creates a new orderer process.
func NewOrdererProcess(t *testing.T, rootDir string, blockSize uint64, blockTimeout time.Duration) *OrdererProcess {
	ports := findAvailablePortRange(t, numPortsForOrderer)
	o := &OrdererProcess{
		Name: "mockorderer",
		Config: &OrdererConfig{
			ServerEndpoint: makeLocalListenAddress(ports[0]),
			BlockSize:      blockSize,
			BlockTimeout:   blockTimeout,
		},
	}

	configFilePath := constructConfigFilePath(rootDir, o.Name, o.Config.ServerEndpoint)
	createConfigFile(t, o.Config, ordererConfigTemplate, configFilePath)

	o.Process = start(ordererCmd, configFilePath, o.Name)

	return o
}
