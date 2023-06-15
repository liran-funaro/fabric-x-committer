package cluster

import (
	"fmt"
	"os"
	"os/exec"
	"path"

	"github.com/tedsuo/ifrit"
)

type ShardServerProcess struct {
	Name            string
	ConfigFilePath  string
	Process         ifrit.Process
	StartPortNumber int
	Config          *ShardServerConfig
	RootDirPath     string
}

type ShardServerConfig struct {
	ServerEndpoint  string
	MetricsEndpoint string
	LatencyEndpoint string
	DatabaseType    string
	DatabaseRootDir string
}

var (
	shardserver_config_template = "../configtemplates/shardserver-config-template.yaml"
	shardserver_cmd             = "../../bin/shardsservice"
)

func NewShardServerProcess(startPortNumber int, rootDir string) *ShardServerProcess {
	shardServerProcess := &ShardServerProcess{
		Name:            "shardserver",
		StartPortNumber: startPortNumber,
		RootDirPath:     rootDir,
	}
	shardServerProcess.createConfigFile()
	shardServerProcess.start()
	return shardServerProcess
}

func (s *ShardServerProcess) createConfigFile() {
	s.Config = &ShardServerConfig{
		ServerEndpoint:  fmt.Sprintf(":%d", s.nextPort()),
		MetricsEndpoint: fmt.Sprintf(":%d", s.nextPort()),
		LatencyEndpoint: fmt.Sprintf(":%d", s.nextPort()),
		DatabaseType:    "goleveldb",
	}
	s.Config.DatabaseRootDir = path.Join(s.RootDirPath, s.Name+s.Config.ServerEndpoint)
	s.ConfigFilePath = constructConfigFilePath(s.RootDirPath, s.Name, s.Config.ServerEndpoint)

	createConfigFile(s.Config, shardserver_config_template, s.ConfigFilePath)
}

func (s *ShardServerProcess) start() {
	cmd := exec.Command(shardserver_cmd, "--configs", s.ConfigFilePath)
	s.Process = run(cmd, s.Name, "Initializing shard instances manager")
}

func (s *ShardServerProcess) nextPort() int {
	s.StartPortNumber++
	return s.StartPortNumber
}

func (s *ShardServerProcess) kill() {
	s.Process.Signal(os.Kill)
}
