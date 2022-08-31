package logger

import (
	"go.uber.org/zap"
)

var logger *zap.SugaredLogger

func init() {
	logger = zap.Must(zap.NewDevelopment(zap.WithCaller(true))).Sugar()

	logger.Infof("Initialized logger...")
}

func NewLogger(name string) *zap.SugaredLogger {
	return logger.Named(name)
}
