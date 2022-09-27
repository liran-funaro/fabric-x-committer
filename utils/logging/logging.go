package logging

import (
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var loggerInstance Logger
var mu sync.Mutex

var logLevelMap = map[Level]zapcore.Level{
	Debug: zap.DebugLevel,
	Info:  zap.InfoLevel,
	Error: zap.ErrorLevel,
}

type Logger struct {
	*zap.SugaredLogger
}

func init() {
	SetupWithConfig(defaultConfig)
}

func SetupWithConfig(config *Config) {
	mu.Lock()
	defer mu.Unlock()

	loggerInstance.SugaredLogger = zap.Must(createLogger(config)).Sugar()
}

func createLogger(config *Config) (*zap.Logger, error) {
	if !config.Enabled {
		return zap.NewNop(), nil
	}

	var options []zap.Option
	if level, ok := logLevelMap[config.Level]; ok {
		options = append(options, zap.IncreaseLevel(level))
	}

	options = append(options, zap.WithCaller(config.Caller))

	if config.Development {
		c := zap.NewDevelopmentConfig()
		c.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		return c.Build(options...)
	}

	return zap.NewProduction(options...)
}

func New(name string) *Logger {
	return &loggerInstance
}
