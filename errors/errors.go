package errors

import (
	"context"
	"net/http"

	"github.com/cyverse-de/go-mod/gotelnats"
	"github.com/cyverse-de/p/go/svcerror"
	"github.com/pkg/errors"
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

func HTTPStatusCode(err error) int {
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

func NatsError(ctx context.Context, err error) *svcerror.ServiceError {
	return gotelnats.InitServiceError(
		ctx, err, &gotelnats.ErrorOptions{
			ErrorCode: NatsStatusCode(err),
		},
	)
}
