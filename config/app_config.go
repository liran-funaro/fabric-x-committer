package config

import (
	"os"
	"path/filepath"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"gopkg.in/yaml.v3"
)

type DeploymentEnv = string

const (
	Dev         DeploymentEnv = "DEV"
	Performance               = "PERF"
	Prod                      = "PROD"
	Default                   = ""
)
const DeploymentEnvKey = "SC_DEPLOYMENT_ENV"

var configFiles = map[DeploymentEnv]string{
	Dev:         "config-dev.yaml",
	Default:     "config-dev.yaml",
	Performance: "config-perf.yaml",
	Prod:        "config-prod.yaml",
}

type LoggingLevel = string

const (
	Debug LoggingLevel = "DEBUG"
	Info               = "INFO"
	Error              = "ERROR"
)

type GlobalConfig struct {
	Logging    LoggingConfig
	Network    NetworkConfig
	Prometheus PrometheusConfig
}
type LoggingConfig struct {
	Enabled     bool
	Level       LoggingLevel
	Caller      bool
	Development bool
}
type NetworkConfig struct {
	DefaultGrpcPort int
}
type PrometheusConfig struct {
	Enabled bool
}

var AppConfig *GlobalConfig

func init() {
	env := os.Getenv(DeploymentEnvKey)
	configFile := filepath.Join(utils.CurrentDir(), configFiles[env])
	AppConfig = newAppConfig(configFile)
}

func newAppConfig(filename string) *GlobalConfig {
	appConfig, err := readConfig(filename)
	if err != nil {
		panic(err)
	}

	err = overwriteEnvVars(appConfig)
	if err != nil {
		panic(err)
	}

	return appConfig
}

func readConfig(absolutePath string) (*GlobalConfig, error) {
	appConfig := &GlobalConfig{}

	file, err := os.ReadFile(absolutePath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(file, appConfig)
	if err != nil {
		return nil, err
	}

	return appConfig, nil
}

func overwriteEnvVars(config *GlobalConfig) error {
	config.Prometheus.Enabled = true
	// TODO: Overwrite config with env vars

	return nil
}
