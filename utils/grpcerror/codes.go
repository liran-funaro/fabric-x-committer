package grpcerror

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// HasCode returns true if the rpcErr holds the given code.
func HasCode(rpcErr error, code codes.Code) bool {
	if rpcErr == nil {
		return false
	}

	errStatus, ok := status.FromError(rpcErr)
	if !ok {
		return false
	}

	return errStatus.Code() == code
}

// WrapInternalError creates a grpc error with a internal status code for a given error.
func WrapInternalError(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.Internal, err.Error()) //nolint:wrapcheck
}

// WrapInvalidArgument creates a grpc error with a invalid argument status code for a given error.
func WrapInvalidArgument(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.InvalidArgument, err.Error()) //nolint:wrapcheck
}

// WrapNotFound creates a grpc error with a not found status code for a given error.
func WrapNotFound(err error) error {
	if err == nil {
		return nil
	}
	return status.Error(codes.NotFound, err.Error()) //nolint:wrapcheck
}

// FilterUnavailableErrorCode rpc error that caused due to transient connectivity issue.
func FilterUnavailableErrorCode(rpcErr error) error {
	if HasCode(rpcErr, codes.Unavailable) {
		return nil
	}

	return rpcErr
}
