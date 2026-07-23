package errors

import (
	"context"
	"net/http"
	"testing"

	"github.com/cyverse-de/p/go/svcerror"
)

// NatsError must populate a valid HTTP StatusCode (alongside the NATS
// ErrorCode) so the HTTP handlers, which report status via
// response.Error.StatusCode, return a real code instead of 0.
func TestNatsErrorPopulatesStatusCode(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		wantStatusCode int32
		wantErrorCode  svcerror.ErrorCode
	}{
		{"addon not found", ErrAddonNotFound, http.StatusNotFound, svcerror.ErrorCode_NOT_FOUND},
		{"subscription addon not found", ErrSubAddonNotFound, http.StatusNotFound, svcerror.ErrorCode_NOT_FOUND},
		{"invalid request body", ErrInvalidRequestBody, http.StatusBadRequest, svcerror.ErrorCode_BAD_REQUEST},
		{"subscription addons exist", ErrSubscriptionAddonsExist, http.StatusConflict, svcerror.ErrorCode_BAD_REQUEST},
		{"unclassified error", New("boom"), http.StatusInternalServerError, svcerror.ErrorCode_INTERNAL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcErr := NatsError(context.Background(), tt.err)
			if svcErr.StatusCode != tt.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", svcErr.StatusCode, tt.wantStatusCode)
			}
			if svcErr.ErrorCode != tt.wantErrorCode {
				t.Errorf("ErrorCode = %v, want %v", svcErr.ErrorCode, tt.wantErrorCode)
			}
			if svcErr.StatusCode == 0 {
				t.Error("StatusCode must never be 0 (the bug this guards against)")
			}
		})
	}
}
