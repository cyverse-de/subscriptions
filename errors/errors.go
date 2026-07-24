package errors

import (
	"context"
	"net/http"

	"github.com/cyverse-de/p/go/svcerror"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	ErrUserNotFound            = errors.New("user name not found")
	ErrInvalidUsername         = errors.New("invalid username")
	ErrInvalidResourceName     = errors.New("invalid resource name")
	ErrInvalidUsageValue       = errors.New("invalid usage value")
	ErrInvalidUpdateType       = errors.New("invalid update type")
	ErrInvalidResourceUnit     = errors.New("invalid resource unit")
	ErrInvalidOperationName    = errors.New("invalid operation name")
	ErrInvalidValueType        = errors.New("invalid value type")
	ErrInvalidValue            = errors.New("invalid value")
	ErrInvalidEffectiveDate    = errors.New("invalid effective date")
	ErrAddonNotFound           = errors.New("add-on not found")
	ErrSubAddonNotFound        = errors.New("subscription add-on not found")
	ErrSubscriptionAddonsExist = errors.New("subscription add-ons exist")
	ErrInvalidRequestBody      = errors.New("required fields are missing from the request body")
)

func New(s string) error {
	return errors.New(s)
}

// ValidationError marks an error as caused by invalid client input. It maps to
// an HTTP 400 / ErrorCode_BAD_REQUEST regardless of the wrapped error's message.
type ValidationError struct{ err error }

func (e *ValidationError) Error() string { return e.err.Error() }

func (e *ValidationError) Unwrap() error { return e.err }

// AsBadRequest wraps err as a ValidationError so it is reported as a bad
// request. It returns nil when err is nil.
func AsBadRequest(err error) error {
	if err == nil {
		return nil
	}
	return &ValidationError{err: err}
}

func HTTPStatusCode(err error) int {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return http.StatusBadRequest
	}

	switch err {
	case ErrUserNotFound:
		return http.StatusNotFound
	case ErrInvalidUsername:
		return http.StatusBadRequest
	case ErrInvalidResourceName:
		return http.StatusBadRequest
	case ErrInvalidUsageValue:
		return http.StatusBadRequest
	case ErrInvalidUpdateType:
		return http.StatusBadRequest
	case ErrInvalidResourceUnit:
		return http.StatusBadRequest
	case ErrInvalidOperationName:
		return http.StatusBadRequest
	case ErrInvalidValueType:
		return http.StatusBadRequest
	case ErrInvalidValue:
		return http.StatusBadRequest
	case ErrInvalidEffectiveDate:
		return http.StatusBadRequest
	case ErrInvalidRequestBody:
		return http.StatusBadRequest
	case ErrAddonNotFound:
		return http.StatusNotFound
	case ErrSubAddonNotFound:
		return http.StatusNotFound
	case ErrSubscriptionAddonsExist:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func NatsStatusCode(err error) svcerror.ErrorCode {
	var ve *ValidationError
	if errors.As(err, &ve) {
		return svcerror.ErrorCode_BAD_REQUEST
	}

	switch err {
	case ErrUserNotFound:
		return svcerror.ErrorCode_NOT_FOUND
	case ErrAddonNotFound:
		return svcerror.ErrorCode_NOT_FOUND
	case ErrSubAddonNotFound:
		return svcerror.ErrorCode_NOT_FOUND
	case ErrInvalidUsername:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidResourceName:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidUsageValue:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidUpdateType:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidResourceUnit:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidOperationName:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidValueType:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidValue:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidEffectiveDate:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrInvalidRequestBody:
		return svcerror.ErrorCode_BAD_REQUEST
	case ErrSubscriptionAddonsExist:
		return svcerror.ErrorCode_BAD_REQUEST
	default:
		return svcerror.ErrorCode_INTERNAL
	}
}

// NatsError builds a *svcerror.ServiceError for err and records it on the
// context's span. It sets both the ErrorCode and the HTTP StatusCode, so the
// handlers that report status via response.Error.StatusCode return a valid
// code instead of 0.
//
// The name predates the removal of this service's NATS transport; the error
// envelope it builds is still what the HTTP responses carry.
func NatsError(ctx context.Context, err error) *svcerror.ServiceError {
	span := trace.SpanFromContext(ctx)
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	return &svcerror.ServiceError{
		ErrorCode:  NatsStatusCode(err),
		StatusCode: int32(HTTPStatusCode(err)),
		Message:    err.Error(),
	}
}
