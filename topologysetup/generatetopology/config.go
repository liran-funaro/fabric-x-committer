package generatetopology

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fabric"
	"github.ibm.com/distributed-trust-research/scalable-committer/topologysetup/fsc"
)

type Config struct {
	Fabric fabric.Config `mapstructure:"fabric"`
	Fsc    fsc.Config    `mapstructure:"fsc"`
}

func ReadConfig() *Config {
	wrapper := new(struct {
		Config *Config `mapstructure:"topology-setup"`
	})
	config.Unmarshal(wrapper, topologysetup.PortsDecoder)
	return wrapper.Config
}
