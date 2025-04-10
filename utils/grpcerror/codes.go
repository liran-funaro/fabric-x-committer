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

// WrapInternalError creates a grpc error with a [codes.Internal] status code for a given error.
func WrapInternalError(err error) error {
	return wrap(codes.Internal, err)
}

// WrapInvalidArgument creates a grpc error with a [codes.InvalidArgument] status code for a given error.
func WrapInvalidArgument(err error) error {
	return wrap(codes.InvalidArgument, err)
}

// WrapCancelled creates a grpc error with a [codes.Canceled] status code for a given error.
func WrapCancelled(err error) error {
	return wrap(codes.Canceled, err)
}

// WrapNotFound creates a grpc error with a [codes.NotFound] status code for a given error.
func WrapNotFound(err error) error {
	return wrap(codes.NotFound, err)
}

func wrap(c codes.Code, err error) error {
	if err == nil {
		return nil
	}
	return status.Error(c, err.Error())
}

// FilterUnavailableErrorCode rpc error that caused due to transient connectivity issue.
func FilterUnavailableErrorCode(rpcErr error) error {
	if HasCode(rpcErr, codes.Unavailable) {
		return nil
	}

	return rpcErr
}
