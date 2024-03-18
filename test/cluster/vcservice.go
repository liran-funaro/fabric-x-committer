package cluster

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

const numPortsPerVCService = 3

// VCServiceProcess represents a vcservice process.
type VCServiceProcess struct {
	Name           string
	ConfigFilePath string
	Process        ifrit.Process
	Ports          []int
	Config         *VCServiceConfig
	RootDirPath    string
	DBEnv          *vcservice.DatabaseTestEnv
}

// VCServiceConfig represents the configuration of the vcservice process.
type VCServiceConfig struct {
	ServerEndpoint  string
	MetricsEndpoint string
	LatencyEndpoint string
	DatabaseHost    string
	DatabasePort    int
	DatabaseName    string
}

var (
	vcserviceConfigTemplate = "../configtemplates/vcservice-config-template.yaml"
	vcserviceCmd            = "../../bin/vcservice"
)

// NewVCServiceProcess creates a new vcservice process.
func NewVCServiceProcess(
	t *testing.T,
	ports []int,
	rootDir string,
	dbEnv *vcservice.DatabaseTestEnv,
) *VCServiceProcess {
	vcserviceProcess := &VCServiceProcess{
		Name:        "vcservice",
		Ports:       ports,
		RootDirPath: rootDir,
		DBEnv:       dbEnv,
	}
	vcserviceProcess.createConfigFile(t, ports)
	vcserviceProcess.start()
	return vcserviceProcess
}

func (s *VCServiceProcess) createConfigFile(t *testing.T, ports []int) {
	require.Len(t, ports, 3)
	s.Config = &VCServiceConfig{
		ServerEndpoint:  fmt.Sprintf(":%d", ports[0]),
		MetricsEndpoint: fmt.Sprintf(":%d", ports[1]),
		LatencyEndpoint: fmt.Sprintf(":%d", ports[2]),
		DatabaseHost:    s.DBEnv.DBConf.Host,
		DatabasePort:    s.DBEnv.DBConf.Port,
		DatabaseName:    s.DBEnv.DBConf.Database,
	}
	s.ConfigFilePath = constructConfigFilePath(s.RootDirPath, s.Name, s.Config.ServerEndpoint)

	createConfigFile(t, s.Config, vcserviceConfigTemplate, s.ConfigFilePath)
}

func (s *VCServiceProcess) start() {
	cmd := exec.Command(vcserviceCmd, "start", "--configs", s.ConfigFilePath)
	s.Process = run(cmd, s.Name, "Serving")
}

func (s *VCServiceProcess) kill() {
	s.Process.Signal(os.Kill)
}
