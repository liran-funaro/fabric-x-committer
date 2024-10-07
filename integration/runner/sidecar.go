package runner

import (
	"testing"

	"github.com/tedsuo/ifrit"
)

type (
	// SidecarProcess represents a sidecar process.
	SidecarProcess struct {
		Name    string
		Process ifrit.Process
		Config  *SidecarConfig
	}

	// SidecarConfig represents the configuration of the sidecar process.
	SidecarConfig struct {
		ServerEndpoint      string
		MetricsEndpoint     string
		LatencyEndpoint     string
		OrdererEndpoint     string
		CoordinatorEndpoint string
		LedgerPath          string
		ChannelID           string
	}
)

const (
	sidecarConfigTemplate = "../configtemplates/sidecar-config-template.yaml"
	sidecarCmd            = "../../bin/sidecar"
	numPortsForSidecar    = 3
)

// NewSidecarProcess creates a new sidecar process.
func NewSidecarProcess( // nolint
	t *testing.T,
	ordererProcess *OrdererProcess,
	coordinatorProcess *CoordinatorProcess,
	rootDir,
	channelID string,
) *SidecarProcess {
	ports := findAvailablePortRange(t, numPortsForSidecar)
	s := &SidecarProcess{
		Name: "sidecar",
		Config: &SidecarConfig{
			ServerEndpoint:      makeLocalListenAddress(ports[0]),
			MetricsEndpoint:     makeLocalListenAddress(ports[1]),
			LatencyEndpoint:     makeLocalListenAddress(ports[2]),
			OrdererEndpoint:     ordererProcess.Config.ServerEndpoint,
			CoordinatorEndpoint: coordinatorProcess.Config.ServerEndpoint,
			LedgerPath:          rootDir,
			ChannelID:           channelID,
		},
	}

	configFilePath := constructConfigFilePath(rootDir, s.Name, s.Config.ServerEndpoint)
	createConfigFile(t, s.Config, sidecarConfigTemplate, configFilePath)

	s.Process = start(sidecarCmd, configFilePath, s.Name)
	return s
}
