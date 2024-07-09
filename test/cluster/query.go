package cluster

import (
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

const numPortsPerQueryService = 3

// QueryServiceProcess represents a query-service process.
type QueryServiceProcess struct {
	Name           string
	ConfigFilePath string
	Process        ifrit.Process
	Ports          []int
	Config         QueryServiceConfig
	RootDirPath    string
	DBEnv          *vcservice.DatabaseTestEnv // DBEnv is a database instance for the QS
}

// QueryServiceConfig represents the configuration of the query-service process.
type QueryServiceConfig struct {
	ServerEndpoint  string
	MetricsEndpoint string
	LatencyEndpoint string
	DatabaseHost    string
	DatabasePort    int
	DatabaseName    string
}

var (
	queryServiceConfigTemplate = "../configtemplates/queryservice-config-template.yaml"
	queryServiceCmd            = "../../bin/queryservice"
)

// NewQueryServiceProcess creates a new query-service process.
func NewQueryServiceProcess(t *testing.T,
	ports []int,
	tempDir string,
	dbEnv *vcservice.DatabaseTestEnv,
) *QueryServiceProcess {
	queryServiceProcess := &QueryServiceProcess{
		Name:        "queryService",
		Ports:       ports,
		RootDirPath: tempDir,
		DBEnv:       dbEnv,
	}
	queryServiceProcess.createConfigFile(t, ports)
	queryServiceProcess.start()
	return queryServiceProcess
}

func (s *QueryServiceProcess) createConfigFile(t *testing.T, ports []int) {
	require.Len(t, ports, numPortsPerQueryService)
	s.Config = QueryServiceConfig{
		ServerEndpoint:  makeLocalListenAddress(ports[0]),
		MetricsEndpoint: makeLocalListenAddress(ports[1]),
		LatencyEndpoint: makeLocalListenAddress(ports[2]),
		DatabaseHost:    s.DBEnv.DBConf.Host,
		DatabasePort:    s.DBEnv.DBConf.Port,
		DatabaseName:    s.DBEnv.DBConf.Database,
	}
	s.ConfigFilePath = constructConfigFilePath(s.RootDirPath, s.Name, s.Config.ServerEndpoint)

	createConfigFile(t, s.Config, queryServiceConfigTemplate, s.ConfigFilePath)
}

func (s *QueryServiceProcess) start() {
	cmd := exec.Command(queryServiceCmd, "start", "--configs", s.ConfigFilePath)
	s.Process = run(cmd, s.Name, "Serving")
}

func (s *QueryServiceProcess) killAndWait(wg *sync.WaitGroup) {
	defer wg.Done()
	if s != nil {
		s.Process.Signal(os.Kill)
		<-s.Process.Wait()
	}
}
