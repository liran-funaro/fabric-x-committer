package runner

import (
	"path"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tedsuo/ifrit"
	configtempl "github.ibm.com/decentralized-trust-research/scalable-committer/config/templates"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

type (
	processWithConfig[T any] struct {
		process ifrit.Process
		config  T
	}
)

const (
	mockordererCmd        = "mockorderingservice"
	queryexecutorCmd      = "queryexecutor"
	signatureverifierCmd  = "signatureverifier"
	validatorpersisterCmd = "validatorpersister"
	coordinatorCmd        = "coordinator"
	sidecarCmd            = "sidecar"
	loadgenCmd            = "loadgen"

	configTemplateRootPath = "../../config/templates"
	configFileExtension    = ".yaml"
	executableRootPath     = "../../bin"
)

func newProcess[T any](t *testing.T, cmdName, rootDir string, config T) *processWithConfig[T] {
	inputConfigTemplateFilePath := path.Join(configTemplateRootPath, cmdName+configFileExtension)
	outputConfigFilePath := constructConfigFilePath(rootDir, cmdName, uuid.NewString())
	require.NoError(t, configtempl.CreateConfigFile(config, inputConfigTemplateFilePath, outputConfigFilePath))
	return &processWithConfig[T]{
		process: start(path.Join(executableRootPath, cmdName), outputConfigFilePath, cmdName),
		config:  config,
	}
}

func newQueryServiceOrVCServiceConfig(
	t *testing.T,
	dbEnv *vcservice.DatabaseTestEnv,
) *configtempl.QueryServiceOrVCServiceConfig {
	return &configtempl.QueryServiceOrVCServiceConfig{
		CommonEndpoints: newCommonEndpoints(t),
		DatabaseHost:    dbEnv.DBConf.Host,
		DatabasePort:    dbEnv.DBConf.Port,
		DatabaseName:    dbEnv.DBConf.Database,
	}
}

func newCommonEndpoints(t *testing.T) configtempl.CommonEndpoints {
	ports := findAvailablePortRange(t, 3)
	return configtempl.CommonEndpoints{
		ServerEndpoint:  makeLocalListenAddress(ports[0]),
		MetricsEndpoint: makeLocalListenAddress(ports[1]),
		LatencyEndpoint: makeLocalListenAddress(ports[2]),
	}
}
