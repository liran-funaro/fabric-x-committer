package config

import (
	"strings"

	"github.com/spf13/viper"
	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/logging"
)

func initializeLoggerViaConfig() error {
	loggerConfig := &logging.Config{
		Enabled:     viper.GetBool("logging.enabled"),
		Level:       strings.ToUpper(viper.GetString("logging.level")),
		Caller:      viper.GetBool("logging.Caller"),
		Development: viper.GetBool("logging.Development"),
	}

	logging.SetupWithConfig(loggerConfig)

	return nil
}

func initializeLoggerForLevel(level logging.Level) {
	logging.SetupWithConfig(&logging.Config{
		Enabled:     true,
		Level:       level,
		Caller:      true,
		Development: true,
	})
}
