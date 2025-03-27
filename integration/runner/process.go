package runner

import (
	"os"
	"os/exec"
	"path"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"

	"github.ibm.com/decentralized-trust-research/scalable-committer/cmd/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/service/vc"
)

type (
	// ProcessWithConfig holds the ifrit process and the corresponding configuration.
	ProcessWithConfig[T any] struct {
		process        ifrit.Process
		config         T
		cmdName        string
		rootDir        string
		configFilePath string
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

	configFileExtension    = ".yaml"
	executableRootPath     = "../../bin"
	configTemplateRootPath = "../../cmd/config/templates"
)

func newProcess[T any](t *testing.T, cmdName, rootDir string, conf T) *ProcessWithConfig[T] {
	t.Helper()
	configFilePath := CreateConfigFromTemplate(t, cmdName, rootDir, conf)
	p := &ProcessWithConfig[T]{
		process:        start(path.Join(executableRootPath, cmdName), configFilePath, cmdName),
		config:         conf,
		cmdName:        cmdName,
		rootDir:        rootDir,
		configFilePath: configFilePath,
	}

	t.Cleanup(func() {
		p.Stop(t)
	})

	return p
}

// Stop stops the running process.
func (p *ProcessWithConfig[T]) Stop(t *testing.T) {
	t.Helper()
	p.process.Signal(os.Kill)
	select {
	case <-p.process.Wait():
	case <-time.After(30 * time.Second):
		t.Errorf("Process [%s] did not terminate after 30 seconds", p.cmdName)
	}
}

// Restart stops the process if it running and then starts it.
func (p *ProcessWithConfig[T]) Restart(t *testing.T) {
	t.Helper()
	p.Stop(t)
	p.process = start(path.Join(executableRootPath, p.cmdName), p.configFilePath, p.cmdName)
	t.Cleanup(func() { p.Stop(t) })
}

// CreateConfigFromTemplate creates a config file using template yaml and returning the output config path.
func CreateConfigFromTemplate[T any](t *testing.T, cmdName, rootDir string, configObj T) string {
	t.Helper()
	inputConfigTemplateFilePath := path.Join(configTemplateRootPath, cmdName+configFileExtension)
	outputConfigFilePath := constructConfigFilePath(rootDir, cmdName, uuid.NewString())
	config.CreateConfigFile(t, configObj, inputConfigTemplateFilePath, outputConfigFilePath)

	return outputConfigFilePath
}

// Run executes the specified command and returns the corresponding process.
// It is important to note that the underlying invocation function (Invoke)
// returns only when either process.Ready or process.Wait has been read.
// Consequently, the caller only needs to read process.Wait to wait for the
// process to complete and capture any errors that may have occurred during execution.
//
//nolint:ireturn
func Run(cmd *exec.Cmd, name, startCheck string) ifrit.Process {
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
	return process
}

func start(cmd, configFilePath, name string) ifrit.Process { //nolint:ireturn
	c := exec.Command(cmd, "start", "--configs", configFilePath)
	return Run(c, name, "")
}

func newQueryServiceOrVCServiceConfig(
	t *testing.T,
	dbEnv *vc.DatabaseTestEnv,
) *config.QueryServiceOrVCServiceConfig {
	t.Helper()
	return &config.QueryServiceOrVCServiceConfig{
		CommonEndpoints:   newCommonEndpoints(t),
		DatabaseEndpoints: dbEnv.DBConf.Endpoints,
		DatabaseName:      dbEnv.DBConf.Database,
		LoadBalance:       dbEnv.DBConf.LoadBalance,
	}
}

func newCommonEndpoints(t *testing.T) config.CommonEndpoints {
	t.Helper()
	ports := findAvailablePortRange(t, 2)
	return config.CommonEndpoints{
		ServerEndpoint:  makeLocalListenAddress(ports[0]),
		MetricsEndpoint: makeLocalListenAddress(ports[1]),
	}
}
