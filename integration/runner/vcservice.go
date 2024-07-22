package runner

import (
	"os"
	"os/exec"
	"sync"
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
	require.Len(t, ports, numPortsPerVCService)
	s.Config = &VCServiceConfig{
		ServerEndpoint:  makeLocalListenAddress(ports[0]),
		MetricsEndpoint: makeLocalListenAddress(ports[1]),
		LatencyEndpoint: makeLocalListenAddress(ports[2]),
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

func (s *VCServiceProcess) killAndWait(wg *sync.WaitGroup) {
	defer wg.Done()
	if s != nil {
		s.Process.Signal(os.Kill)
		<-s.Process.Wait()
	}
}
