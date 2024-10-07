package runner

import (
	"testing"

	"github.com/tedsuo/ifrit"
)

type (
	// CoordinatorProcess represents a coordinator process.
	CoordinatorProcess struct {
		Name    string
		Process ifrit.Process
		Config  *CoordinatorConfig
	}

	// CoordinatorConfig represents the configuration of the coordinator process.
	CoordinatorConfig struct {
		ServerEndpoint       string
		MetricsEndpoint      string
		LatencyEndpoint      string
		SigVerifierEndpoints []string
		VCServiceEndpoints   []string
	}
)

const (
	coordinatorConfigTemplate = "../configtemplates/coordinator-config-template.yaml"
	coordinatorCmd            = "../../bin/coordinator"
	numPortsForCoordinator    = 3
)

// NewCoordinatorProcess creates a new coordinator process.
func NewCoordinatorProcess(
	t *testing.T,
	sigVerifierProcesses []*SigVerifierProcess,
	vcserviceProcesses []*VCServiceProcess,
	rootDir string,
) *CoordinatorProcess {
	ports := findAvailablePortRange(t, numPortsForCoordinator)
	c := &CoordinatorProcess{
		Name: "coordinator",
		Config: &CoordinatorConfig{
			ServerEndpoint:  makeLocalListenAddress(ports[0]),
			MetricsEndpoint: makeLocalListenAddress(ports[1]),
			LatencyEndpoint: makeLocalListenAddress(ports[2]),
		},
	}

	for _, sigVerifierProcess := range sigVerifierProcesses {
		c.Config.SigVerifierEndpoints = append(c.Config.SigVerifierEndpoints, sigVerifierProcess.Config.ServerEndpoint)
	}

	for _, shardServerProcess := range vcserviceProcesses {
		c.Config.VCServiceEndpoints = append(c.Config.VCServiceEndpoints, shardServerProcess.Config.ServerEndpoint)
	}

	configFilePath := constructConfigFilePath(rootDir, c.Name, c.Config.ServerEndpoint)
	createConfigFile(t, c.Config, coordinatorConfigTemplate, configFilePath)

	c.Process = start(coordinatorCmd, configFilePath, c.Name)

	return c
}
