package metrics

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type Config struct {
	Enable   bool                 `mapstructure:"enable"`
	Endpoint *connection.Endpoint `mapstructure:"endpoint"`
}
