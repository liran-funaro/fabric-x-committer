package connection

import (
	"context"
	"errors"
	"io"
	"regexp"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const maxMsgSize = 100 * 1024 * 1024

type DialConfig struct {
	Endpoint
	DialOpts []grpc.DialOption
}

var knownConnectionIssues = regexp.MustCompile(`(?i)EOF|connection\s+refused|closed\s+network\s+connection`)

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

func IsStreamEnd(rpcErr error) bool {
	errStatus, ok := status.FromError(rpcErr)
	if !ok {
		return errors.Is(rpcErr, io.EOF) || errors.Is(rpcErr, context.Canceled) || errors.Is(rpcErr, context.DeadlineExceeded)
	}

	rpcErrCode := errStatus.Code()
	if rpcErrCode == codes.Canceled || rpcErrCode == codes.DeadlineExceeded {
		return true
	}

	if rpcErrCode == codes.Unavailable && knownConnectionIssues.MatchString(errStatus.Message()) {
		return true
	}

	return false
}

func FilterStreamErrors(err error) error {
	code := status.Code(err)
	if errors.Is(err, io.EOF) || code == codes.Canceled || code == codes.DeadlineExceeded {
		return nil
	}
	return err
}

func WrapStreamRpcError(rpcErr error) error {
	if IsStreamEnd(rpcErr) {
		return nil
	}
	return rpcErr
}
