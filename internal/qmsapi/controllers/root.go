package controllers

import (
	"database/sql"
	"net/http"

	"github.com/cyverse-de/go-mod/logging"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

var log = logging.Log.WithFields(logrus.Fields{"package": "controllers"})

// Server defines the REST API of the qms
type Server struct {
	Router         *echo.Echo
	DB             *sql.DB
	GORMDB         *gorm.DB
	Service        string
	Title          string
	Version        string
	ReportOverages bool
	UsernameSuffix string
}

// ServerInfo returns basic information about the server.
func (s Server) ServerInfo() *model.RootResponse {
	return &model.RootResponse{
		Service: s.Service,
		Title:   s.Title,
		Version: s.Version,
	}
}

// RootHandler handles GET requests to the / endpoint.
//
// swagger:route GET / misc getRoot
//
// # General API Information
//
// Lists general information about the service API itself.
//
// responses:
//
//	200: rootResponse
func (s Server) RootHandler(ctx echo.Context) error {
	return model.Success(ctx, s.ServerInfo(), http.StatusOK)
}

// V1RootHandler handles GET requests to the /v1 endpoint.
//
// swagger:route GET /v1 misc getV1Root
//
// # General API Version 1 Information
//
// Lists general information about version 1 of the service API.
//
// responses:
//
//	200: apiVersionResponse
func (s Server) V1RootHandler(ctx echo.Context) error {
	resp := model.APIVersionResponse{
		RootResponse: *s.ServerInfo(),
		APIVersion:   "1.0.0",
	}
	return model.Success(ctx, resp, http.StatusOK)
}
