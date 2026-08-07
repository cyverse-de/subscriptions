package model

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

type RootResponse struct {

	// The name of the service
	Service string `json:"service"`

	// The service title
	Title string `json:"title"`

	// The service version
	Version string `json:"version"`
}

type APIVersionResponse struct {
	RootResponse

	// The API version
	APIVersion string `json:"api_version"`
}

// Response wrapper for all response bodies.
type Response struct {
	Result interface{} `json:"result,omitempty"`
	Error  string      `json:"error,omitempty"`
	Status string      `json:"status"`
}

// SuccessResponse Basic Success Response
func SuccessResponse(data interface{}, status int) Response {
	return Response{
		Result: data,
		Status: http.StatusText(status),
	}
}

// SuccessMessage sends a basic success response containing a textual message to the caller.
func SuccessMessage(ctx echo.Context, msg string, status int) error {
	return ctx.JSON(status, SuccessResponse(msg, status))
}

// Success sends a basic success response to the caller.
func Success(ctx echo.Context, data interface{}, status int) error {
	return ctx.JSON(status, SuccessResponse(data, status))
}

// ErrorResponse Basic error response
func ErrorResponse(errStr string, status int) Response {
	return Response{
		Error:  errStr,
		Status: http.StatusText(status),
	}
}

// Error sends a basic error response to the caller.
func Error(ctx echo.Context, errStr string, status int) error {
	return ctx.JSON(status, ErrorResponse(errStr, status))
}

// HTTPError sends a basic error response to the caller for the given instance of echo.HTTPError.
func HTTPError(ctx echo.Context, err *echo.HTTPError) error {
	return ctx.JSON(err.Code, ErrorResponse(err.Error(), err.Code))
}
