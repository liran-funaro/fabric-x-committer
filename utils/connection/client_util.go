package connection

import (
	"errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const maxMsgSize = 100 * 1024 * 1024

type DialConfig struct {
	Endpoint
	DialOpts []grpc.DialOption
}

func NewDialConfig(endpoint Endpoint) *DialConfig {
	return NewDialConfigWithCreds(endpoint, insecure.NewCredentials())
}

func NewDialConfigWithCreds(endpoint Endpoint, creds credentials.TransportCredentials) *DialConfig {
	return &DialConfig{
		Endpoint: endpoint,
		DialOpts: []grpc.DialOption{
			grpc.WithTransportCredentials(creds),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxMsgSize),
				grpc.MaxCallSendMsgSize(maxMsgSize),
			),
		},
	}
}

func OpenConnections(endpoints []*Endpoint, transportCredentials credentials.TransportCredentials) ([]*grpc.ClientConn, error) {
	logger.Infof("Opening connections to %d endpoints: %v.\n", len(endpoints), endpoints)
	connections := make([]*grpc.ClientConn, len(endpoints))
	for i, endpoint := range endpoints {
		conn, err := Connect(NewDialConfigWithCreds(*endpoint, transportCredentials))

		if err != nil {
			logger.Errorf("Error connecting: %v", err)
			closeErrs := CloseConnections(connections[:i])
			if closeErrs != nil {
				logger.Error(closeErrs)
			}
			return nil, err
		}

		connections[i] = conn
	}
	logger.Infof("Opened %d connections", len(connections))
	return connections, nil
}

func CloseConnections(connections []*grpc.ClientConn) error {
	logger.Infof("Closing %d connections.\n", len(connections))
	errs := make([]error, 0, len(connections))
	for i, closer := range connections {
		errs[i] = closer.Close()
	}
	return errors.Join(errs...)
}

func Connect(config *DialConfig) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(config.Endpoint.Address(), config.DialOpts...)

	if err != nil {
		logger.Errorf("Error connecting to %s: %v", config.Endpoint.String(), err)
		return nil, err
	}
	logger.Debugf("Successfully connected to %s", config.Endpoint.String())
	return conn, nil
}
