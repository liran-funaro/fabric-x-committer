package cluster

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/tedsuo/ifrit"
)

type SigVerifierProcess struct {
	Name            string
	ConfigFilePath  string
	Process         ifrit.Process
	StartPortNumber int
	Config          *SigVerifierConfig
	RootDirPath     string
}

type SigVerifierConfig struct {
	ServerEndpoint  string
	MetricsEndpoint string
	LatencyEndpoint string
}

var (
	sigverifier_config_template = "../configtemplates/sigverifier-config-template.yaml"
	sigverifier_cmd             = "../../bin/sigservice"
)

func NewSigVerifierProcess(startPortNumber int, tempDir string) *SigVerifierProcess {
	sigVerifierProcess := &SigVerifierProcess{
		Name:            "sigverifier",
		StartPortNumber: startPortNumber,
		RootDirPath:     tempDir,
	}
	sigVerifierProcess.createConfigFile()
	sigVerifierProcess.start()
	return sigVerifierProcess
}

func (s *SigVerifierProcess) createConfigFile() {
	s.Config = &SigVerifierConfig{
		ServerEndpoint:  fmt.Sprintf(":%d", s.nextPort()),
		MetricsEndpoint: fmt.Sprintf(":%d", s.nextPort()),
		LatencyEndpoint: fmt.Sprintf(":%d", s.nextPort()),
	}
	s.ConfigFilePath = constructConfigFilePath(s.RootDirPath, s.Name, s.Config.ServerEndpoint)

	createConfigFile(s.Config, sigverifier_config_template, s.ConfigFilePath)
}

func (s *SigVerifierProcess) start() {
	cmd := exec.Command(sigverifier_cmd, "--configs", s.ConfigFilePath)
	s.Process = run(cmd, s.Name, "Was created and initialized with")
}

func (s *SigVerifierProcess) nextPort() int {
	s.StartPortNumber++
	return s.StartPortNumber
}

func (s *SigVerifierProcess) kill() {
	s.Process.Signal(os.Kill)
}
