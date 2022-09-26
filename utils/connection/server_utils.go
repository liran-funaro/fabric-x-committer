package connection

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

type Host = string

type ServerConfig struct {
	Prometheus Prometheus
	Endpoint   Endpoint
	Opts       []grpc.ServerOption
}

type Prometheus struct {
	Enabled  bool     `mapstructure:"enabled"`
	Endpoint Endpoint `mapstructure:"endpoint"`
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

	if serverConfig.Prometheus.Enabled {
		go launchPrometheus(serverConfig.Prometheus.Endpoint)
	}

	err = grpcServer.Serve(listener)
	if err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

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

type Endpoint struct {
	Host Host `mapstructure:"host"`
	Port int  `mapstructure:"port"`
}

const endpointSplitter = ":"

func (e *Endpoint) Address() string {
	return fmt.Sprintf("%s%s%d", e.Host, endpointSplitter, e.Port)
}

func NewEndpoint(value string) (*Endpoint, error) {
	vals := strings.Split(value, ":")
	if len(vals) != 2 {
		return nil, errors.New("not in format: 1.2.3.4:5")
	}
	host := vals[0]
	port, err := strconv.Atoi(vals[1])
	if err != nil {
		return nil, err
	}
	return &Endpoint{Host: host, Port: port}, nil
}

func EndpointVar(p *Endpoint, name string, defaultValue Endpoint, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(endpoint string) error {
		result, err := NewEndpoint(endpoint)
		if err != nil {
			return err
		}
		*p = *result
		return nil
	})
}

const flagSliceSeparator = ","

func EndpointVars(p *[]*Endpoint, name string, defaultValue []*Endpoint, usage string) {
	*p = defaultValue
	flag.Func(name, usage, func(input string) error {
		endpoints := strings.Split(input, flagSliceSeparator)
		results := make([]*Endpoint, len(endpoints))
		for i, endpoint := range endpoints {
			result, err := NewEndpoint(endpoint)
			if err != nil {
				return err
			}
			results[i] = result
		}
		*p = results
		return nil
	})
}

func launchPrometheus(endpoint Endpoint) {
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(endpoint.Address(), nil)
	if err != nil {
		panic(err)
	}
}
