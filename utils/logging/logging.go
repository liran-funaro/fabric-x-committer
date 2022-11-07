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

	loggerInstance.SugaredLogger = createLogger(config).Sugar()
}

func createLogger(config *Config) *zap.Logger {
	if !config.Enabled {
		return zap.NewNop()
	}

	defaultLevel, ok := logLevelMap[config.Level]
	if !ok {
		defaultLevel = zapcore.ErrorLevel
	}
	outputs := []string{"stderr"}
	if config.Output != "" {
		outputs = append(outputs, config.Output)
	}

	c := zap.Config{
		Level:       zap.NewAtomicLevelAt(defaultLevel),
		Development: config.Development,
		Sampling: &zap.SamplingConfig{
			Initial:    100,
			Thereafter: 100,
		},
		Encoding:         "console",
		EncoderConfig:    getEncoderConfig(config.Development),
		OutputPaths:      outputs,
		ErrorOutputPaths: outputs,
	}

	return zap.Must(c.Build(zap.WithCaller(config.Caller)))
}

func getEncoderConfig(dev bool) zapcore.EncoderConfig {
	var cfg zapcore.EncoderConfig
	if dev {
		cfg = zap.NewDevelopmentEncoderConfig()
		cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		cfg = zap.NewProductionEncoderConfig()
	}
	cfg.EncodeTime = zapcore.ISO8601TimeEncoder
	return cfg
}

func New(name string) *Logger {
	return &loggerInstance
}
