package logging

import (
	"io"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc/grpclog"
)

var (
	loggerInstance Logger
	mu             sync.Mutex
)

type Logger struct {
	*zap.SugaredLogger
}

func init() {
	SetupWithConfig(defaultConfig)
}

func SetupWithConfig(config *Config) {
	mu.Lock()
	defer mu.Unlock()

	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, os.Stderr))
	loggerInstance.SugaredLogger = createLogger(config).Sugar()
}

func createLogger(config *Config) *zap.Logger {
	if config == nil || !config.Enabled {
		return zap.NewNop()
	}

	level := zap.NewAtomicLevel()
	switch strings.ToUpper(config.Level) {
	case Debug:
		level.SetLevel(zap.DebugLevel)
	case Info:
		level.SetLevel(zap.InfoLevel)
	case Warning:
		level.SetLevel(zap.WarnLevel)
	case Error:
		level.SetLevel(zap.ErrorLevel)
	}
	outputs := []string{"stderr"}
	if config.Output != "" {
		outputs = append(outputs, config.Output)
	}

	c := zap.Config{
		Level:       level,
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

	return zap.Must(c.Build(zap.WithCaller(config.Caller))).Named(config.Name)
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
	cfg.EncodeName = zapcore.FullNameEncoder
	return cfg
}

func New(name string) *Logger {
	// Due to package initialization order, the Fabric package that initialize the GRPC logger
	// might be initialized after our own package.
	// Since methods calls can happen only after package initialization,
	// this call ensures the GRPC logger will stay quiet.
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, os.Stderr))
	return &loggerInstance
}

// ErrorStackTrace prints the stack trace present in the error type.
func (l *Logger) ErrorStackTrace(err error) {
	if err == nil {
		return
	}
	l.Errorf("%+v", err)
}
