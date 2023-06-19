package connection

import (
	"crypto/tls"
	"log"
	"net"
	"sync"
	"time"

	"github.ibm.com/distributed-trust-research/scalable-committer/utils"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
)

const grpcProtocol = "tcp"

type ServerConfig struct {
	Endpoint  Endpoint               `mapstructure:"endpoint"`
	Creds     *ServerCredsConfig     `mapstructure:"creds"`
	KeepAlive *ServerKeepAliveConfig `mapstructure:"keep-alive"`
}

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

func RunServerMain(serverConfig *ServerConfig, register func(server *grpc.Server, port int)) {
	logger.Infof("Running server with:\n"+
		"\tEndpoint: %s:%s\n"+
		"\tTLS Creds: %v\n"+
		"\tKeep Alive: %v\n", grpcProtocol, serverConfig.Endpoint.Address(), serverConfig.Creds, serverConfig.KeepAlive)

	listener, err := net.Listen(grpcProtocol, serverConfig.Endpoint.Address())
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(serverConfig.Opts()...)
	register(grpcServer, listener.Addr().(*net.TCPAddr).Port)

	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func RunServerMainAndWait(serverConfig *ServerConfig, register func(server *grpc.Server, port int)) {
	serverStarted := sync.WaitGroup{}
	serverStarted.Add(1)

	go RunServerMain(serverConfig, func(server *grpc.Server, port int) {
		register(server, port)
		serverStarted.Done()
	})

	serverStarted.Wait() // Avoid trying to connect before the server starts
}
