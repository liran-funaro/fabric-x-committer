package logging

import (
	"github.ibm.com/distributed-trust-research/scalable-committer/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var logger *zap.SugaredLogger

func init() {

	logger = zap.Must(createLogger(config.AppConfig.Logging)).Sugar()

	logger.Infof("Initialized logger...")
}

var logLevelMap = map[config.LoggingLevel]zapcore.Level{
	config.Debug: zap.DebugLevel,
	config.Info:  zap.InfoLevel,
	config.Error: zap.ErrorLevel,
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

func New(name string) *zap.SugaredLogger {
	return logger.Named(name)
}
