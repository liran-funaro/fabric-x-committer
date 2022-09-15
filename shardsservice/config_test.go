package shardsservice

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReadConfig(t *testing.T) {
	expectedConfig := &Configuration{
		Database: &DatabaseConf{
			Name:    "rocksdb",
			RootDir: "./",
		},
		Limits: &LimitsConf{
			MaxGoroutines:                     100,
			MaxPhaseOneResponseBatchItemCount: 100,
			PhaseOneResponseCutTimeout:        50 * time.Millisecond,
		},
	}

	config, err := ReadConfig("./testdata")
	require.NoError(t, err)
	require.Equal(t, expectedConfig, config)
}
