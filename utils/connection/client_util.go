package connection

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.ibm.com/decentralized-trust-research/scalable-committer/utils/monitoring/promutil"
)

const (
	maxMsgSize = 100 * 1024 * 1024
	// Connected indicates that the connection to the service is currently established.
	Connected = 1.0
	// Disconnected indicates that the connection to the service is currently not established.
	Disconnected = 0
)

type (
	DialConfig struct {
		WithAddress
		DialOpts []grpc.DialOption
	}

	WithAddress interface {
		Address() string
	}
)

// GrpcConfig defines the retry policy for a gRPC client connection.
// This policy differs from grpc.WithBlock(), which only blocks during the initial connection.
// The retry policy applies to all subsequent gRPC calls made through the client connection.
// Our GRPC retry policy is applicable only for the following status codes:
//
//	(1) UNAVAILABLE	The service is currently unavailable (e.g., transient network issue, server down).
//	(2) DEADLINE_EXCEEDED	Operation took too long (deadline passed).
//	(3) RESOURCE_EXHAUSTED	Some resource (e.g., quota) has been exhausted; the operation cannot proceed.
//
//go:embed grpc_config.json
var GrpcConfig string

var knownConnectionIssues = regexp.MustCompile(`(?i)EOF|connection\s+refused|closed\s+network\s+connection`)

func NewDialConfig(endpoint WithAddress) *DialConfig {
	return NewDialConfigWithCreds(endpoint, insecure.NewCredentials())
}

func NewDialConfigWithCreds(endpoint WithAddress, creds credentials.TransportCredentials) *DialConfig {
	return &DialConfig{
		WithAddress: endpoint,
		DialOpts:    NewDialOptionWithCreds(creds),
	}
}

func NewDialOptionWithCreds(creds credentials.TransportCredentials) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
		grpc.WithBlock(),
		grpc.WithReturnConnectionError(),
	}
}

func OpenConnections[T WithAddress](endpoints []T, transportCredentials credentials.TransportCredentials) ([]*grpc.ClientConn, error) {
	return openConnections(endpoints, transportCredentials, Connect)
}

func CloseConnections[T io.Closer](connections ...T) error {
	logger.Infof("Closing %d connections.\n", len(connections))
	errs := make([]error, len(connections))
	for i, closer := range connections {
		errs[i] = filterAcceptableCloseErr(closer.Close())
	}
	return errors.Join(errs...)
}

var acceptableCloseErr = []error{
	io.EOF,
	io.ErrClosedPipe,
	io.ErrUnexpectedEOF,
	net.ErrClosed,
	context.Canceled,
	context.DeadlineExceeded,
}

func filterAcceptableCloseErr(err error) error {
	if err == nil {
		return nil
	}
	for _, e := range acceptableCloseErr {
		if errors.Is(err, e) {
			return nil
		}
	}
	return err
}

func CloseConnectionsLog[T io.Closer](connections ...T) {
	closeErr := CloseConnections(connections...)
	if closeErr != nil {
		logger.Errorf("failed closing connections: %v", closeErr)
	}
}

func Connect(config *DialConfig) (*grpc.ClientConn, error) {
	config.DialOpts = append(config.DialOpts, grpc.WithDefaultServiceConfig(GrpcConfig))
	ctx, cancel := context.WithTimeout(context.TODO(), 90*time.Second)
	defer cancel()

	address := config.WithAddress.Address()
	cc, err := grpc.DialContext(ctx, address, config.DialOpts...)
	if err != nil {
		logger.Errorf("Error connecting to %s: %v", address, err)
		return nil, err
	}

	return cc, nil
}

func LazyConnect(config *DialConfig) (*grpc.ClientConn, error) {
	address := config.WithAddress.Address()
	config.DialOpts = append(config.DialOpts, grpc.WithDefaultServiceConfig(GrpcConfig))
	cc, err := grpc.NewClient(address, config.DialOpts...)
	if err != nil {
		logger.Errorf("Error connecting to %s: %v", address, err)
		return nil, err
	}
	return cc, nil
}

func OpenLazyConnections[T WithAddress](
	endpoints []T,
	transportCredentials credentials.TransportCredentials,
) ([]*grpc.ClientConn, error) {
	return openConnections(endpoints, transportCredentials, LazyConnect)
}

func openConnections[T WithAddress](
	endpoints []T,
	transportCredentials credentials.TransportCredentials,
	connect func(*DialConfig) (*grpc.ClientConn, error),
) ([]*grpc.ClientConn, error) {
	logger.Infof("Opening connections to %d endpoints: %v.\n", len(endpoints), endpoints)
	connections := make([]*grpc.ClientConn, len(endpoints))
	for i, endpoint := range endpoints {
		conn, err := connect(NewDialConfigWithCreds(endpoint, transportCredentials))
		if err != nil {
			logger.Errorf("Error connecting: %v", err)
			CloseConnectionsLog(connections[:i]...)
			return nil, err
		}

		connections[i] = conn
	}
	logger.Infof("Opened %d connections", len(connections))
	return connections, nil
}

// WaitForConnection waits for the given connection to be in the ready state.
func WaitForConnection(
	ctx context.Context,
	conn *grpc.ClientConn,
	connStatus *prometheus.GaugeVec,
	failureCount *prometheus.CounterVec,
) {
	label := []string{conn.CanonicalTarget()}
	defer func() {
		promutil.SetGaugeVec(connStatus, label, Connected)
	}()

	if conn.GetState() == connectivity.Ready {
		return
	}

	promutil.AddToCounterVec(failureCount, label, 1)
	promutil.SetGaugeVec(connStatus, label, Disconnected)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logger.Debug("Reconnecting to service.")
			conn.Connect()
			if conn.GetState() == connectivity.Ready {
				return
			}
			logger.Debug("service connection is not ready.")
		}
	}
}

// FilterStreamRPCError filters RPC errors that caused due to ending stream.
func FilterStreamRPCError(rpcErr error) error {
	if rpcErr == nil {
		return nil
	}

	if IsStreamEnd(rpcErr) {
		return nil
	}
	return rpcErr
}

// IsStreamEnd returns true if an RPC error indicates stream end.
func IsStreamEnd(rpcErr error) bool {
	if rpcErr == nil {
		return false
	}

	if IsStreamContextEnd(rpcErr) {
		return true
	}

	errStatus, ok := status.FromError(rpcErr)
	if !ok {
		return errors.Is(rpcErr, io.EOF)
	}

	rpcErrCode := errStatus.Code()
	if rpcErrCode == codes.Unavailable && knownConnectionIssues.MatchString(errStatus.Message()) {
		return true
	}

	return false
}

// IsStreamContextEnd returns true if an RPC error indicates stream context end.
func IsStreamContextEnd(rpcErr error) bool {
	if rpcErr == nil {
		return false
	}

	errStatus, ok := status.FromError(rpcErr)
	if !ok {
		return errors.Is(rpcErr, context.Canceled) || errors.Is(rpcErr, context.DeadlineExceeded)
	}

	rpcErrCode := errStatus.Code()
	if rpcErrCode == codes.Canceled || rpcErrCode == codes.DeadlineExceeded {
		return true
	}

	return false
}

// AddressString returns the addresses as a string with comma as a separator between them.
func AddressString[T WithAddress](addresses ...T) string {
	listOfAddresses := make([]string, len(addresses))
	for i, address := range addresses {
		listOfAddresses[i] = address.Address()
	}
	return strings.Join(listOfAddresses, ",")
}
