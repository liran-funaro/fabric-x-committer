package connection

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/cockroachdb/errors"
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

	// ServerKeepAliveConfig describes the keep alive parameters.
	ServerKeepAliveConfig struct {
		Params            *ServerKeepAliveParamsConfig            `mapstructure:"params"`
		EnforcementPolicy *ServerKeepAliveEnforcementPolicyConfig `mapstructure:"enforcement-policy"`
	}

	// ServerKeepAliveParamsConfig describes the keep alive policy.
	ServerKeepAliveParamsConfig struct {
		MaxConnectionIdle     time.Duration `mapstructure:"max-connection-idle"`
		MaxConnectionAge      time.Duration `mapstructure:"max-connection-age"`
		MaxConnectionAgeGrace time.Duration `mapstructure:"max-connection-age-grace"`
		Time                  time.Duration `mapstructure:"time"`
		Timeout               time.Duration `mapstructure:"timeout"`
	}

	// ServerKeepAliveEnforcementPolicyConfig describes the keep alive enforcement policy.
	ServerKeepAliveEnforcementPolicyConfig struct {
		MinTime             time.Duration `mapstructure:"min-time"`
		PermitWithoutStream bool          `mapstructure:"permit-without-stream"`
	}

	// ServerCredsConfig describes the server's credentials configuration.
	ServerCredsConfig struct {
		CertPath string `mapstructure:"cert-path"`
		KeyPath  string `mapstructure:"key-path"`
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

// NewLocalHostServer returns a default server config with endpoint "localhost:0".
func NewLocalHostServer() *ServerConfig {
	return &ServerConfig{Endpoint: *NewLocalHost()}
}

func (c *ServerConfig) opts() []grpc.ServerOption {
	var opts []grpc.ServerOption
	if c.Creds != nil {
		cert, err := tls.LoadX509KeyPair(c.Creds.CertPath, c.Creds.KeyPath)
		utils.Must(err)
		opts = append(opts, grpc.Creds(credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.NoClientCert,
			MinVersion:   tls.VersionTLS12,
		})))
	}
	if c.KeepAlive != nil && c.KeepAlive.Params != nil {
		opts = append(opts, grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     c.KeepAlive.Params.MaxConnectionIdle,
			MaxConnectionAge:      c.KeepAlive.Params.MaxConnectionAge,
			MaxConnectionAgeGrace: c.KeepAlive.Params.MaxConnectionAgeGrace,
			Time:                  c.KeepAlive.Params.Time,
			Timeout:               c.KeepAlive.Params.Timeout,
		}))
	}
	if c.KeepAlive != nil && c.KeepAlive.EnforcementPolicy != nil {
		opts = append(opts, grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             c.KeepAlive.EnforcementPolicy.MinTime,
			PermitWithoutStream: c.KeepAlive.EnforcementPolicy.PermitWithoutStream,
		}))
	}
	return opts
}

// Listen instantiate a [net.Listener] and updates the config port with the effective port.
func Listen(config *ServerConfig) (net.Listener, error) {
	listener, err := net.Listen(grpcProtocol, config.Endpoint.Address())
	if err != nil {
		return nil, errors.Wrap(err, "failed to listen")
	}

	addr := listener.Addr()
	tcpAddress, ok := addr.(*net.TCPAddr)
	if !ok {
		return nil, errors.Join(errors.New("failed to cast to TCP address"), listener.Close())
	}
	config.Endpoint.Port = tcpAddress.Port

	logger.Infof("Running server at: %s://%s", grpcProtocol, config.Endpoint.String())
	return listener, nil
}

// NewGrpcServer instantiate a [grpc.Server] and [net.Listener].
func NewGrpcServer(config *ServerConfig) (*grpc.Server, net.Listener, error) {
	listener, err := Listen(config)
	if err != nil {
		return nil, nil, err
	}
	server := grpc.NewServer(config.opts()...)
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
