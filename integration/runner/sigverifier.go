package runner

import (
	"testing"

	"github.com/tedsuo/ifrit"
)

type (
	// SigVerifierProcess represents a sigverifier process.
	SigVerifierProcess struct {
		Name    string
		Process ifrit.Process
		Config  *SigVerifierConfig
	}

	// SigVerifierConfig represents the configuration of the sigverifier process.
	SigVerifierConfig struct {
		ServerEndpoint  string
		MetricsEndpoint string
		LatencyEndpoint string
	}
)

const (
	sigverifierConfigTemplate = "../configtemplates/sigverifier-config-template.yaml"
	sigverifierCmd            = "../../bin/signatureverifier"
	numPortsForSigVerifier    = 3
)

// NewSigVerifierProcess creates a new sigverifier process.
func NewSigVerifierProcess(t *testing.T, tempDir string) *SigVerifierProcess {
	ports := findAvailablePortRange(t, numPortsForSigVerifier)
	s := &SigVerifierProcess{
		Name: "sigverifier",
		Config: &SigVerifierConfig{
			ServerEndpoint:  makeLocalListenAddress(ports[0]),
			MetricsEndpoint: makeLocalListenAddress(ports[1]),
			LatencyEndpoint: makeLocalListenAddress(ports[2]),
		},
	}

	configFilePath := constructConfigFilePath(tempDir, s.Name, s.Config.ServerEndpoint)
	createConfigFile(t, s.Config, sigverifierConfigTemplate, configFilePath)

	s.Process = start(sigverifierCmd, configFilePath, s.Name)
	return s
}
