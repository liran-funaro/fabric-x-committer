package runner

import (
	"os"
	"os/exec"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
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
	configtempl.CreateConfigFile(t, config, inputConfigTemplateFilePath, outputConfigFilePath)
	p := start(path.Join(executableRootPath, cmdName), outputConfigFilePath, cmdName)
	t.Cleanup(func() {
		p.Signal(os.Kill)
		select {
		case <-p.Wait():
		case <-time.After(30 * time.Second):
			t.Errorf("Process [%s] did not terminate after 30 seconds", cmdName)
		}
	})
	return &processWithConfig[T]{
		process: p,
		config:  config,
	}
}

func run(cmd *exec.Cmd, name, startCheck string) ifrit.Process { //nolint:ireturn
	p := ginkgomon.New(ginkgomon.Config{
		Command:           cmd,
		Name:              name,
		AnsiColorCode:     "",
		StartCheck:        startCheck,
		StartCheckTimeout: 0,
		Cleanup: func() {
		},
	})
	process := ifrit.Invoke(p)
	gomega.Eventually(process.Ready(), 3*time.Minute, 1*time.Second).Should(gomega.BeClosed())
	return process
}

func start(cmd, configFilePath, name string) ifrit.Process { //nolint:ireturn
	c := exec.Command(cmd, "start", "--configs", configFilePath)
	return run(c, name, "Serving")
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
		LoadBalance:     dbEnv.DBConf.LoadBalance,
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
