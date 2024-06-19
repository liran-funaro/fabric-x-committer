package cluster

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
)

const numPortsPerSigVerifier = 3

// SigVerifierProcess represents a sigverifier process.
type SigVerifierProcess struct {
	Name           string
	ConfigFilePath string
	Process        ifrit.Process
	Ports          []int
	Config         *SigVerifierConfig
	RootDirPath    string
}

// SigVerifierConfig represents the configuration of the sigverifier process.
type SigVerifierConfig struct {
	ServerEndpoint  string
	MetricsEndpoint string
	LatencyEndpoint string
}

var (
	sigverifierConfigTemplate = "../configtemplates/sigverifier-config-template.yaml"
	sigverifierCmd            = "../../bin/sigservice"
)

// NewSigVerifierProcess creates a new sigverifier process.
func NewSigVerifierProcess(t *testing.T, ports []int, tempDir string) *SigVerifierProcess {
	sigVerifierProcess := &SigVerifierProcess{
		Name:        "sigverifier",
		Ports:       ports,
		RootDirPath: tempDir,
	}
	sigVerifierProcess.createConfigFile(t, ports)
	sigVerifierProcess.start()
	return sigVerifierProcess
}

func (s *SigVerifierProcess) createConfigFile(t *testing.T, ports []int) {
	require.Len(t, ports, 3)
	s.Config = &SigVerifierConfig{
		ServerEndpoint:  makeLocalListenAddress(ports[0]),
		MetricsEndpoint: makeLocalListenAddress(ports[1]),
		LatencyEndpoint: makeLocalListenAddress(ports[2]),
	}
	s.ConfigFilePath = constructConfigFilePath(s.RootDirPath, s.Name, s.Config.ServerEndpoint)

	createConfigFile(t, s.Config, sigverifierConfigTemplate, s.ConfigFilePath)
}

func (s *SigVerifierProcess) start() {
	cmd := exec.Command(sigverifierCmd, "--configs", s.ConfigFilePath)
	s.Process = run(cmd, s.Name, "Was created and initialized with")
}

func (s *SigVerifierProcess) kill() {
	s.Process.Signal(os.Kill)
}
