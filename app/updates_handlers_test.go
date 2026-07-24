package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// AddUserUpdateHTTPHandler assigns the path username into request.Update.User before validateUpdate runs,
// so a body missing either nested object has to be rejected in the handler itself.
func TestAddUserUpdateHandlerRejectsMissingNestedObject(t *testing.T) {
	e := echo.New()
	app := &App{}

	tests := []struct {
		name string
		body string
	}{
		{"empty body", `{}`},
		{"update without a user", `{"update":{"value_type":"usages"}}`},
		{"null update", `{"update":null}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(tt.body))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)
			c.SetParamNames("username")
			c.SetParamValues("someuser")

			if err := app.AddUserUpdateHTTPHandler(c); err != nil {
				t.Fatalf("handler returned an error: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}
