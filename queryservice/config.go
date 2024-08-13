package queryservice

import (
	"time"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/config"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/connection"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring"
	"github.ibm.com/decentralized-trust-research/scalable-committer/vcservice"
)

// Config is the configuration for the query service.
// To reduce the applied workload on the database and improve performance we batch views and queries.
// That is, views with the same protoqueryservice.ViewParameters will be batched together
// if they created within the ViewAggregationWindow. But no more than MaxAggregatedViews can be
// batched together.
// Queries from the same view and namespace will be batched together.
// A query batch is ready to be submitted if it has more keys than
// MinBatchKeys, or the batch has waited more than MaxBatchWait.
// Once a batch is ready, it is submitted as soon as there is a connection available,
// up the maximal number of connections defined in the Database configuration.
// Thus, a batch query can wait longer than MaxBatchWait if we don't have available connection.
// To avoid dangling views, a view is limited to a period of MaxViewTimeout.
// MaxViewTimeout includes the time it takes to execute the last query in the view.
// That is, if a query is executed while the timeout is expired, the query will be aborted.
// The number of parallel active views is theoretically unlimited as multiple views can be aggregated
// together. However, the number of active batched views is limited by the maximal
// number of database connections.
// Setting the maximal database connections higher than the following, ensures enough available connections.
// (MaxViewTimeout / ViewAggregationWindow) * <number-of-used-view-configuration-permutations>
// If there are no more available connections, queries will wait until such connection is available.
type Config struct {
	Server                *connection.ServerConfig  `mapstructure:"server"`
	Monitoring            *monitoring.Config        `mapstructure:"monitoring"`
	Database              *vcservice.DatabaseConfig `mapstructure:"database"`
	MinBatchKeys          int                       `mapstructure:"min-batch-keys"`
	MaxBatchWait          time.Duration             `mapstructure:"max-batch-wait"`
	ViewAggregationWindow time.Duration             `mapstructure:"view-aggregation-window"`
	MaxAggregatedViews    int                       `mapstructure:"max-aggregated-viewIDToViewHolder"`
	MaxViewTimeout        time.Duration             `mapstructure:"max-view-timeout"`
}

// ReadConfig reads the configuration from the viper instance.
// If the configuration file is used, the caller should call
// config.ReadFromYamlFile() before calling this function.
func ReadConfig() *Config {
	setDefaults()

	wrapper := new(struct {
		Config Config `mapstructure:"query-service"`
	})
	config.Unmarshal(wrapper)
	return &wrapper.Config
}

func setDefaults() {
	// defaults for ServerConfig
	viper.SetDefault("query-service.server.endpoint", "localhost:7003")

	// defaults for DatabaseConfig
	prefix := "query-service.database."
	viper.SetDefault(prefix+"host", "localhost")
	viper.SetDefault(prefix+"port", 5433)
	viper.SetDefault(prefix+"username", "yugabyte")
	viper.SetDefault(prefix+"password", "yugabyte")
	viper.SetDefault(prefix+"database", "yugabyte")
	viper.SetDefault(prefix+"max-connections", 20)
	viper.SetDefault(prefix+"min-connections", 10)

	// defaults for monitoring.config
	prefix = "query-service.monitoring."
	viper.SetDefault(prefix+"metrics.endpoint", "localhost:7004")
	viper.SetDefault(prefix+"metrics.enable", true)

	// defaults for monitoring.config
	prefix = "query-service."
	viper.SetDefault(prefix+"min-batch-keys", 1024)
	viper.SetDefault(prefix+"max-batch-wait", 100*time.Millisecond)
	viper.SetDefault(prefix+"view-aggregation-window", 100*time.Millisecond)
	viper.SetDefault(prefix+"max-aggregated-viewIDToViewHolder", 1024)
	viper.SetDefault(prefix+"max-view-timeout", 10*time.Second)
}
