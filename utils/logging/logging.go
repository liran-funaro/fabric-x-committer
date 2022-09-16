package logging

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logger AppLogger

type AppLogger struct {
	*zap.SugaredLogger
}

func init() {
	updateLogger()
	config.OnConfigUpdated(updateLogger)
}

var logLevelMap = map[config.LoggingLevel]zapcore.Level{
	config.Debug: zap.DebugLevel,
	config.Info:  zap.InfoLevel,
	config.Error: zap.ErrorLevel,
}

func updateLogger() {
	logger.SugaredLogger = zap.Must(createLogger(config.AppConfig.Logging)).Sugar()
}

func createLogger(c config.LoggingConfig) (*zap.Logger, error) {
	if !c.Enabled {
		return zap.NewNop(), nil
	}
	var options []zap.Option
	if level, ok := logLevelMap[c.Level]; ok {
		options = append(options, zap.IncreaseLevel(level))
	}
	if c.Caller {
		options = append(options, zap.AddCaller())
	}

	if c.Development {
		return zap.NewDevelopment(options...)
	}
	return zap.NewProduction(options...)
}

func New(name string) *AppLogger {
	return &logger
}
