package shardsservice

import (
	"os"
	"path"
	"time"

	"github.com/pkg/errors"
	"github.com/spf13/viper"
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

const (
	defaultLocalConfigFile = "config.yml"
)

type Configuration struct {
	Database *DatabaseConf
	Limits   *LimitsConf
}

type DatabaseConf struct {
	Name    string
	RootDir string
}

type LimitsConf struct {
	MaxGoroutines                     uint32
	MaxPhaseOneResponseBatchItemCount uint32
	PhaseOneResponseCutTimeout        time.Duration
}

// Read reads configurations from the config file and returns the config
func ReadConfig(configFilePath string) (*Configuration, error) {
	if configFilePath == "" {
		return nil, errors.New("path to the configuration file is empty")
	}

	fileInfo, err := os.Stat(configFilePath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to read the status of the configuration path: '%s'", configFilePath)
	}

	fileName := configFilePath
	if fileInfo.IsDir() {
		fileName = path.Join(configFilePath, defaultLocalConfigFile)
	}

	v := viper.New()
	v.SetConfigFile(fileName)

	v.SetDefault("database.name", "rocksdb")
	v.SetDefault("database.rootDir", "./")
	v.SetDefault("limits.maxGoroutines", 100)
	v.SetDefault("limits.maxPhaseOneResponseBatchItemCount", 100)
	v.SetDefault("limits.phaseOneResponseCutTimeout", 50*time.Millisecond)

	if err := v.ReadInConfig(); err != nil {
		return nil, errors.Wrap(err, "error reading local config file")
	}

	conf := &Configuration{}
	if err := v.UnmarshalExact(conf); err != nil {
		return nil, errors.Wrapf(err, "unable to unmarshal local config file: '%s' into struct", fileName)
	}
	return conf, nil

}

var configWrapper struct {
	Config struct {
		Prometheus connection.Prometheus `mapstructure:"prometheus"`
		Endpoint   connection.Endpoint   `mapstructure:"endpoint"`
	} `mapstructure:"shards-service"`
}

var Config = &configWrapper.Config

func init() {
	config.Unmarshal(&configWrapper)
}
