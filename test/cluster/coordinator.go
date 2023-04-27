package cluster

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/tedsuo/ifrit"
)

type CoordinatorProcess struct {
	Name            string
	ConfigFilePath  string
	Process         ifrit.Process
	StartPortNumber int
	Config          *CoordinatorConfig
	RootDirPath     string
}

type CoordinatorConfig struct {
	ServerEndpoint       string
	MetricsEndpoint      string
	LatencyEndpoint      string
	SigVerifierEndpoints []string
	ShardServerEndpoints []*ShardConfig
}

type ShardConfig struct {
	Endpoint  string
	NumShards int
}

var (
	coordinator_config_template = "../configtemplates/coordinator-config-template.yaml"
	coordinator_cmd             = "../../bin/coordinator"
)

func NewCoordinatorProcess(startPortNumber int, sigVerifierProcesses []*SigVerifierProcess, shardServerProcesses []*ShardServerProcess, numShards int, rootDir string) *CoordinatorProcess {
	c := &CoordinatorProcess{
		Name:            "coordinator",
		ConfigFilePath:  "",
		Process:         nil,
		StartPortNumber: startPortNumber,
		RootDirPath:     rootDir,
	}
	c.createConfigFile(sigVerifierProcesses, shardServerProcesses, numShards)
	c.start()
	return c
}

func (c *CoordinatorProcess) createConfigFile(sigVerifierProcesses []*SigVerifierProcess, shardServerProcesses []*ShardServerProcess, numShards int) {
	c.Config = &CoordinatorConfig{
		ServerEndpoint:  fmt.Sprintf(":%d", c.nextPort()),
		MetricsEndpoint: fmt.Sprintf(":%d", c.nextPort()),
		LatencyEndpoint: fmt.Sprintf(":%d", c.nextPort()),
	}

	for _, sigVerifierProcess := range sigVerifierProcesses {
		c.Config.SigVerifierEndpoints = append(c.Config.SigVerifierEndpoints, sigVerifierProcess.Config.ServerEndpoint)
	}

	for _, shardServerProcess := range shardServerProcesses {
		c.Config.ShardServerEndpoints = append(c.Config.ShardServerEndpoints, &ShardConfig{
			Endpoint:  shardServerProcess.Config.ServerEndpoint,
			NumShards: numShards,
		})
	}

	c.ConfigFilePath = constructConfigFilePath(c.RootDirPath, c.Name, c.Config.ServerEndpoint)

	createConfigFile(c.Config, coordinator_config_template, c.ConfigFilePath)
}

func (c *CoordinatorProcess) start() {
	cmd := exec.Command(coordinator_cmd, "--configs", c.ConfigFilePath)
	c.Process = run(cmd, c.Name, "Running server with")
}

func (c *CoordinatorProcess) nextPort() int {
	c.StartPortNumber++
	return c.StartPortNumber
}

func (c *CoordinatorProcess) kill() {
	c.Process.Signal(os.Kill)
}
