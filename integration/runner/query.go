package runner

import (
	"testing"

	"github.com/tedsuo/ifrit"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

type (
	// QueryServiceProcess represents a query-service process.
	QueryServiceProcess struct {
		Name    string
		Process ifrit.Process
		Config  QueryServiceConfig
	}

	// QueryServiceConfig represents the configuration of the query-service process.
	QueryServiceConfig struct {
		ServerEndpoint  string
		MetricsEndpoint string
		LatencyEndpoint string
		DatabaseHost    string
		DatabasePort    int
		DatabaseName    string
	}
)

const (
	queryServiceConfigTemplate = "../configtemplates/queryservice-config-template.yaml"
	queryServiceCmd            = "../../bin/queryexecutor"
	numPortsForQueryService    = 3
)

// NewQueryServiceProcess creates a new query-service process.
func NewQueryServiceProcess(t *testing.T, tempDir string, dbEnv *vcservice.DatabaseTestEnv) *QueryServiceProcess {
	ports := findAvailablePortRange(t, numPortsForQueryService)
	q := &QueryServiceProcess{
		Name: "queryService",
		Config: QueryServiceConfig{
			ServerEndpoint:  makeLocalListenAddress(ports[0]),
			MetricsEndpoint: makeLocalListenAddress(ports[1]),
			LatencyEndpoint: makeLocalListenAddress(ports[2]),
			DatabaseHost:    dbEnv.DBConf.Host,
			DatabasePort:    dbEnv.DBConf.Port,
			DatabaseName:    dbEnv.DBConf.Database,
		},
	}

	configFilePath := constructConfigFilePath(tempDir, q.Name, q.Config.ServerEndpoint)
	createConfigFile(t, q.Config, queryServiceConfigTemplate, configFilePath)

	q.Process = start(queryServiceCmd, configFilePath, q.Name)
	return q
}
