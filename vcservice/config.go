package vcservice

import (
	"fmt"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
)

// ValidatorCommitterServiceConfig is the configuration for the validator-committer service.
type ValidatorCommitterServiceConfig struct {
	Server         *connection.ServerConfig `mapstructure:"server"`
	Database       *DatabaseConfig          `mapstructure:"database"`
	ResourceLimits *ResourceLimitsConfig    `mapstructure:"resource-limits"`
	Monitoring     *monitoring.Config       `mapstructure:"monitoring"`
}

// DatabaseConfig is the configuration for the database.
type DatabaseConfig struct {
	Host           string                   `mapstructure:"host"`
	Port           int                      `mapstructure:"port"`
	Username       string                   `mapstructure:"username"`
	Password       string                   `mapstructure:"password"`
	Database       string                   `mapstructure:"database"`
	MaxConnections int32                    `mapstructure:"max-connections"`
	MinConnections int32                    `mapstructure:"min-connections"`
	LoadBalance    bool                     `mapstructure:"load-balance"`
	Retry          *connection.RetryProfile `mapstructure:"retry"`
}

// DataSourceName returns the data source name of the database.
func (d *DatabaseConfig) DataSourceName() string {
	ret := fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
		d.Username, d.Password, d.Host, d.Port, d.Database)

	// The load balancing flag is only available when the server supports it (having multiple nodes).
	// Thus, we only add it when explicitly required. Otherwise, an error will occur.
	if d.LoadBalance {
		ret += "&load_balance=true"
	}
	return ret
}

// ResourceLimitsConfig is the configuration for the resource limits.
type ResourceLimitsConfig struct {
	MaxWorkersForPreparer             int           `mapstructure:"max-workers-for-preparer"`
	MaxWorkersForValidator            int           `mapstructure:"max-workers-for-validator"`
	MaxWorkersForCommitter            int           `mapstructure:"max-workers-for-committer"`
	MinTransactionBatchSize           int           `mapstructure:"min-transaction-batch-size"`
	TimeoutForMinTransactionBatchSize time.Duration `mapstructure:"timeout-for-min-transaction-batch-size"`
}
