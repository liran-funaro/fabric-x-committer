package serverconfig

import (
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/parallelexecutor"
	"github.ibm.com/decentralized-trust-research/scalable-committer/sigverification/signature"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

type SigVerificationConfig struct {
	Monitoring       monitoring.Config        `mapstructure:"monitoring"`
	Server           *connection.ServerConfig `mapstructure:"server"`
	Scheme           signature.Scheme         `mapstructure:"scheme"`
	ParallelExecutor parallelexecutor.Config  `mapstructure:"parallel-executor"`
}
