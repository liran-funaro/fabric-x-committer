package cluster

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
)

const numPortsForCoordinator = 3

// CoordinatorProcess represents a coordinator process.
type CoordinatorProcess struct {
	Name           string
	ConfigFilePath string
	Process        ifrit.Process
	Ports          []int
	Config         *CoordinatorConfig
	RootDirPath    string
}

// CoordinatorConfig represents the configuration of the coordinator process.
type CoordinatorConfig struct {
	ServerEndpoint       string
	MetricsEndpoint      string
	LatencyEndpoint      string
	SigVerifierEndpoints []string
	VCServiceEndpoints   []string
}

var (
	coordinatorConfigTemplate = "../configtemplates/coordinator-config-template.yaml"
	coordinatorCmd            = "../../bin/coordinator"
)

// NewCoordinatorProcess creates a new coordinator process.
func NewCoordinatorProcess( //nolint:revive
	t *testing.T,
	ports []int,
	sigVerifierProcesses []*SigVerifierProcess,
	vcserviceProcesses []*VCServiceProcess,
	rootDir string,
) *CoordinatorProcess {
	c := &CoordinatorProcess{
		Name:           "coordinator",
		ConfigFilePath: "",
		Process:        nil,
		Ports:          ports,
		RootDirPath:    rootDir,
	}
	c.createConfigFile(t, ports, sigVerifierProcesses, vcserviceProcesses)
	c.start()
	return c
}

func (c *CoordinatorProcess) createConfigFile(
	t *testing.T,
	ports []int,
	sigVerifierProcesses []*SigVerifierProcess,
	vcserviceProcesses []*VCServiceProcess,
) {
	require.Len(t, ports, 3)
	c.Config = &CoordinatorConfig{
		ServerEndpoint:  fmt.Sprintf(":%d", ports[0]),
		MetricsEndpoint: fmt.Sprintf(":%d", ports[1]),
		LatencyEndpoint: fmt.Sprintf(":%d", ports[2]),
	}

	for _, sigVerifierProcess := range sigVerifierProcesses {
		c.Config.SigVerifierEndpoints = append(c.Config.SigVerifierEndpoints, sigVerifierProcess.Config.ServerEndpoint)
	}

	for _, shardServerProcess := range vcserviceProcesses {
		c.Config.VCServiceEndpoints = append(c.Config.VCServiceEndpoints, shardServerProcess.Config.ServerEndpoint)
	}

	c.ConfigFilePath = constructConfigFilePath(c.RootDirPath, c.Name, c.Config.ServerEndpoint)

	createConfigFile(t, c.Config, coordinatorConfigTemplate, c.ConfigFilePath)
}

func (c *CoordinatorProcess) start() {
	cmd := exec.Command(coordinatorCmd, "start", "--configs", c.ConfigFilePath)
	c.Process = run(cmd, c.Name, "Serving")
}

func (c *CoordinatorProcess) kill() {
	c.Process.Signal(os.Kill)
}
