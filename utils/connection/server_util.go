package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const grpcProtocol = "tcp"

type (
	// ServerConfig describes the connection parameter for a server.
	ServerConfig struct {
		Endpoint  Endpoint               `mapstructure:"endpoint"`
		Creds     *ServerCredsConfig     `mapstructure:"creds"`
		KeepAlive *ServerKeepAliveConfig `mapstructure:"keep-alive"`
	}

	// Service describes the method that are required for a service to run.
	Service interface {
		// Run executes the service until the context is done.
		Run(ctx context.Context) error
		// WaitForReady waits for the service resources to initialize.
		// If the context ended before the service is ready, returns false.
		WaitForReady(ctx context.Context) bool
	}
)

func (c *ServerConfig) Opts() []grpc.ServerOption {
	opts := make([]grpc.ServerOption, 0)
	if c.Creds != nil {
		opts = append(opts, c.Creds.serverOption())
	}
	if c.KeepAlive != nil && c.KeepAlive.Params != nil {
		opts = append(opts, c.KeepAlive.Params.serverOption())
	}
	if c.KeepAlive != nil && c.KeepAlive.EnforcementPolicy != nil {
		opts = append(opts, c.KeepAlive.EnforcementPolicy.serverOption())
	}
	return opts
}

type ServerKeepAliveConfig struct {
	Params            *ServerKeepAliveParamsConfig            `mapstructure:"params"`
	EnforcementPolicy *ServerKeepAliveEnforcementPolicyConfig `mapstructure:"enforcement-policy"`
}
type ServerKeepAliveParamsConfig struct {
	MaxConnectionIdle     time.Duration `mapstructure:"max-connection-idle"`
	MaxConnectionAge      time.Duration `mapstructure:"max-connection-age"`
	MaxConnectionAgeGrace time.Duration `mapstructure:"max-connection-age-grace"`
	Time                  time.Duration `mapstructure:"time"`
	Timeout               time.Duration `mapstructure:"timeout"`
}

func (c *ServerKeepAliveParamsConfig) serverOption() grpc.ServerOption {
	return grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle:     c.MaxConnectionIdle,
		MaxConnectionAge:      c.MaxConnectionAge,
		MaxConnectionAgeGrace: c.MaxConnectionAgeGrace,
		Time:                  c.Time,
		Timeout:               c.Timeout,
	})
}

type ServerKeepAliveEnforcementPolicyConfig struct {
	MinTime             time.Duration `mapstructure:"min-time"`
	PermitWithoutStream bool          `mapstructure:"permit-without-stream"`
}

func (c *ServerKeepAliveEnforcementPolicyConfig) serverOption() grpc.ServerOption {
	return grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
		MinTime:             c.MinTime,
		PermitWithoutStream: c.PermitWithoutStream,
	})
}

type ServerCredsConfig struct {
	CertPath string `mapstructure:"cert-path"`
	KeyPath  string `mapstructure:"key-path"`
}

func (c *ServerCredsConfig) serverOption() grpc.ServerOption {
	cert, err := tls.LoadX509KeyPair(c.CertPath, c.KeyPath)
	utils.Must(err)
	creds := grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.NoClientCert,
	}))
	return creds
}

func NewGrpcServer(config *ServerConfig) (*grpc.Server, net.Listener, error) {
	logger.Infof("Running server at: %s://%s", grpcProtocol, config.Endpoint.Address())
	listener, err := net.Listen(grpcProtocol, config.Endpoint.Address())
	if err != nil {
		return nil, nil, err
	}

	server := grpc.NewServer(config.Opts()...)
	return server, listener, nil
}

// RunGrpcServerMainWithError runs a server and returns error if failed.
func RunGrpcServerMainWithError(
	ctx context.Context,
	serverConfig *ServerConfig,
	register func(server *grpc.Server),
) error {
	server, listener, err := NewGrpcServer(serverConfig)
	if err != nil {
		return err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	serverConfig.Endpoint.Port = port
	register(server)

	g, gCtx := errgroup.WithContext(ctx)
	logger.Infof("Serving...")
	g.Go(func() error {
		return server.Serve(listener)
	})
	<-gCtx.Done()
	server.Stop()
	return g.Wait()
}

// StartService runs a service, waits until it is ready, and register the gRPC server.
// It will stop if either the service ended or its respective gRPC server.
func StartService(
	ctx context.Context,
	service Service,
	serverConfig *ServerConfig,
	register func(server *grpc.Server),
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		// If the service stops, there is no reason to continue the GRPC server.
		defer cancel()
		return service.Run(gCtx)
	})

	ctxTimeout, cancelTimeout := context.WithTimeout(gCtx, 5*time.Minute) // TODO: make this configurable.
	defer cancelTimeout()
	if !service.WaitForReady(ctxTimeout) {
		cancel()
		return fmt.Errorf("service is not ready: %w", g.Wait())
	}

	g.Go(func() error {
		// If the GRPC server stops, there is no reason to continue the service.
		defer cancel()
		return RunGrpcServerMainWithError(gCtx, serverConfig, func(server *grpc.Server) {
			register(server)
		})
	})
	return g.Wait()
}
