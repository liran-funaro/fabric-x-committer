package serverconfig

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/distributed-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/distributed-trust-research/scalable-committer/utils/connection"
)

var configWrapper struct {
	Config struct {
		Prometheus       connection.Prometheus   `mapstructure:"prometheus"`
		Endpoint         connection.Endpoint     `mapstructure:"endpoint"`
		Scheme           signature.Scheme        `mapstructure:"scheme"`
		ParallelExecutor parallelexecutor.Config `mapstructure:"parallel-executor"`
	} `mapstructure:"sig-verification"`
}

var Config = &configWrapper.Config

func init() {
	config.Unmarshal(&configWrapper)
}
