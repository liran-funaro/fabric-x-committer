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
	config.Unmarshal(&configWrapper)
	updateLogger()
	config.OnConfigUpdated(updateLogger)
}

var logLevelMap = map[Level]zapcore.Level{
	Debug: zap.DebugLevel,
	Info:  zap.InfoLevel,
	Error: zap.ErrorLevel,
}

func updateLogger() {
	logger.SugaredLogger = zap.Must(createLogger()).Sugar()
}

func createLogger() (*zap.Logger, error) {
	if !Config.Enabled {
		return zap.NewNop(), nil
	}
	var options []zap.Option
	if level, ok := logLevelMap[Config.Level]; ok {
		options = append(options, zap.IncreaseLevel(level))
	}
	if Config.Caller {
		options = append(options, zap.AddCaller())
	}

	if Config.Development {
		return zap.NewDevelopment(options...)
	}
	return zap.NewProduction(options...)
}

func New(name string) *AppLogger {
	return &logger
}
