package runner

import (
	"testing"

	"github.com/tedsuo/ifrit"

	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

type (
	// VCServiceProcess represents a vcservice process.
	VCServiceProcess struct {
		Name    string
		Process ifrit.Process
		Config  *VCServiceConfig
	}

	// VCServiceConfig represents the configuration of the vcservice process.
	VCServiceConfig struct {
		ServerEndpoint  string
		MetricsEndpoint string
		LatencyEndpoint string
		DatabaseHost    string
		DatabasePort    int
		DatabaseName    string
	}
)

const (
	vcserviceConfigTemplate = "../configtemplates/vcservice-config-template.yaml"
	vcserviceCmd            = "../../bin/validatorpersister"
	numPortsForVCService    = 3
)

// NewVCServiceProcess creates a new vcservice process.
func NewVCServiceProcess(t *testing.T, rootDir string, dbEnv *vcservice.DatabaseTestEnv) *VCServiceProcess {
	ports := findAvailablePortRange(t, numPortsForVCService)
	v := &VCServiceProcess{
		Name: "vcservice",
		Config: &VCServiceConfig{
			ServerEndpoint:  makeLocalListenAddress(ports[0]),
			MetricsEndpoint: makeLocalListenAddress(ports[1]),
			LatencyEndpoint: makeLocalListenAddress(ports[2]),
			DatabaseHost:    dbEnv.DBConf.Host,
			DatabasePort:    dbEnv.DBConf.Port,
			DatabaseName:    dbEnv.DBConf.Database,
		},
	}

	configFilePath := constructConfigFilePath(rootDir, v.Name, v.Config.ServerEndpoint)
	createConfigFile(t, v.Config, vcserviceConfigTemplate, configFilePath)

	v.Process = start(vcserviceCmd, configFilePath, v.Name)
	return v
}
