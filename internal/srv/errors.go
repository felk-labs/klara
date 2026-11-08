package srv

import (
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ErrAborted          ErrorCode = ErrorCode(codes.Aborted)
	ErrAlreadyExists    ErrorCode = ErrorCode(codes.AlreadyExists)
	ErrConflict         ErrorCode = ErrorCode(codes.FailedPrecondition)
	ErrInternal         ErrorCode = ErrorCode(codes.Internal)
	ErrInvalidData      ErrorCode = ErrorCode(codes.InvalidArgument)
	ErrNotFound         ErrorCode = ErrorCode(codes.NotFound)
	ErrPermissionDenied ErrorCode = ErrorCode(codes.PermissionDenied)
	ErrUnauthenticated  ErrorCode = ErrorCode(codes.Unauthenticated)
	ErrUnavailable      ErrorCode = ErrorCode(codes.Unavailable)
	ErrUnknown          ErrorCode = ErrorCode(codes.Unknown)
)

type ErrorCode codes.Code

func (c ErrorCode) Error() string {
	switch c {
	case ErrAlreadyExists:
		return "resource already exists"
	case ErrAborted:
		return "operation aborted"
	case ErrConflict:
		return "data conflict"
	case ErrInternal:
		return "internal error"
	case ErrInvalidData:
		return "invalid data"
	case ErrNotFound:
		return "resource not found"
	case ErrPermissionDenied:
		return "forbidden"
	case ErrUnauthenticated:
		return "not authenticated"
	case ErrUnavailable:
		return "unavailable"
	default:
		return "unknown error"
	}
}

type Error struct {
	code   ErrorCode
	err    error
	status *status.Status
}

func (e *Error) Code() ErrorCode {
	return e.code
}

func (e *Error) Error() string {
	if e.err != nil {
		return e.err.Error()
	}

	return e.code.Error()
}

func (e *Error) GRPCStatus() *status.Status {
	return e.status
}

func (e *Error) Unwrap() error {
	return e.err
}

func NewError(code ErrorCode, err error) error {
	return &Error{
		code:   code,
		err:    err,
		status: status.New(codes.Code(code), code.Error()),
	}
}

func NewErrorf(code ErrorCode, format string, a ...any) error {
	return &Error{
		code:   code,
		err:    fmt.Errorf(format, a...),
		status: status.New(codes.Code(code), fmt.Sprintf(format, a...)),
	}
}
