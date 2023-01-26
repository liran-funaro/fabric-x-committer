package connection

import (
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

func Connect(config *DialConfig) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(config.Endpoint.Address(), config.DialOpts...)

	if err != nil {
		logger.Infof("Error connecting to %s: %v", config.Endpoint.String(), err)
		return nil, err
	}
	return conn, nil
}
