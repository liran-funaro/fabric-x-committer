package monitoring

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
)

type Config struct {
	Server *connection.ServerConfig `mapstructure:"server"`
}
