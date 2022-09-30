package connection

import (
	"flag"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// Server

type ServerConfig struct {
	Endpoint Endpoint
	Opts     []grpc.ServerOption
}

const grpcProtocol = "tcp"

func RunServerMain(serverConfig *ServerConfig, register func(*grpc.Server)) {
	flag.Parse()

	listener, err := net.Listen(grpcProtocol, serverConfig.Endpoint.Address())
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(serverConfig.Opts...)
	register(grpcServer)

	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

// Client

type DialConfig struct {
	Endpoint
	Credentials credentials.TransportCredentials
}

func NewDialConfig(endpoint Endpoint) *DialConfig {
	return &DialConfig{
		Endpoint:    endpoint,
		Credentials: insecure.NewCredentials(),
	}
}

func Connect(config *DialConfig) (*grpc.ClientConn, error) {
	opts := []grpc.DialOption{grpc.WithTransportCredentials(config.Credentials)}

	conn, err := grpc.Dial(config.Endpoint.Address(), opts...)

	if err != nil {
		return nil, err
	}
	return conn, nil
}
