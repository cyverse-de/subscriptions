package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

// The add-on write handlers must reject a body missing its nested object with a
// 400 rather than panicking on a nil dereference (the lax JSON decoder accepts
// such bodies). A zero-value App is enough: each guard returns before touching
// the database.
func TestAddonHandlersRejectMissingNestedObject(t *testing.T) {
	e := echo.New()
	app := &App{}

	tests := []struct {
		name    string
		params  map[string]string
		handler func(echo.Context) error
	}{
		{"add addon", nil, app.AddAddonHTTPHandler},
		{"update addon", map[string]string{"uuid": "abc"}, app.UpdateAddonHTTPHandler},
		{"update subscription addon", nil, app.UpdateSubscriptionAddonHTTPHandler},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
			req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
			rec := httptest.NewRecorder()
			c := e.NewContext(req, rec)

			names := make([]string, 0, len(tt.params))
			values := make([]string, 0, len(tt.params))
			for k, v := range tt.params {
				names = append(names, k)
				values = append(values, v)
			}
			c.SetParamNames(names...)
			c.SetParamValues(values...)

			if err := tt.handler(c); err != nil {
				t.Fatalf("handler returned an error: %v", err)
			}
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}
