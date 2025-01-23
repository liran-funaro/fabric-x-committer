package logging

import "google.golang.org/grpc/grpclog"

func init() {
	NoGrpcLog()
}

// NoGrpcLog prevents logging from grpc.
// This resolves the over logging caused by fabric logger.
func NoGrpcLog() {
	grpclog.SetLoggerV2(&NilLogger{})
}

type NilLogger struct{}

func (g *NilLogger) Info(...any) {
}

func (g *NilLogger) Infoln(...any) {
}

func (g *NilLogger) Infof(string, ...any) {
}

func (g *NilLogger) InfoDepth(int, ...any) {
}

func (g *NilLogger) Warning(...any) {
}

func (g *NilLogger) Warningln(...any) {
}

func (g *NilLogger) Warningf(string, ...any) {
}

func (g *NilLogger) WarningDepth(int, ...any) {
}

func (g *NilLogger) Error(...any) {
}

func (g *NilLogger) Errorln(...any) {
}

func (g *NilLogger) Errorf(string, ...any) {
}

func (g *NilLogger) ErrorDepth(int, ...any) {
}

func (g *NilLogger) Fatal(...any) {
}

func (g *NilLogger) Fatalln(...any) {
}

func (g *NilLogger) Fatalf(string, ...any) {
}

func (g *NilLogger) FatalDepth(int, ...any) {
}

func (g *NilLogger) V(int) bool {
	return true
}
