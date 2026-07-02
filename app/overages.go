package app

import (
	"context"
	"net/http"

	"github.com/cyverse-de/go-mod/pbinit"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/subscriptions/db"
	serrors "github.com/cyverse-de/subscriptions/errors"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func (a *App) getUserOverages(ctx context.Context, request *qms.AllUserOveragesRequest) *qms.OverageList {
	response := pbinit.NewOverageList()

	username, err := a.FixUsername(request.Username)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// If s.ReportOverages is false, then return the empty list of overages that was just created.
	if !a.ReportOverages {
		return response
	}

	d := db.New(a.db)

	results, err := d.GetUserOverages(ctx, username)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	response.Overages = []*qms.Overage{}
	for _, r := range results {
		quota := r.QuotaValue
		usage := r.UsageValue

		if usage >= quota {
			response.Overages = append(response.Overages, &qms.Overage{
				ResourceName: r.ResourceType.Name,
				Quota:        quota,
				Usage:        usage,
			})
		}
	}

	return response
}

func (a *App) GetUserOverages(subject, reply string, request *qms.AllUserOveragesRequest) {
	var err error

	log := log.WithFields(logrus.Fields{"context": "list overages"})

	ctx, span := pbinit.InitAllUserOveragesRequest(request, subject)
	defer span.End()

	response := a.getUserOverages(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) GetUserOveragesHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &qms.AllUserOveragesRequest{
		Username: c.Param("username"),
	}

	response := a.getUserOverages(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) checkUserOverages(ctx context.Context, request *qms.IsOverageRequest) *qms.IsOverage {
	response := pbinit.NewIsOverage()

	if !a.ReportOverages {
		response.IsOverage = false
		return response
	}

	username, err := a.FixUsername(request.Username)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	log = log.WithFields(logrus.Fields{"user": username})

	d := db.New(a.db)

	overages, err := d.GetUserOverages(ctx, username)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}
	if len(overages) > 0 {
		for _, overage := range overages {
			if overage.ResourceType.Name == request.GetResourceName() {
				response.IsOverage = true
			}
		}
	} else {
		response.IsOverage = false
	}

	return response
}

func (a *App) CheckUserOverages(subject, reply string, request *qms.IsOverageRequest) {
	var err error

	log := log.WithFields(logrus.Fields{"context": "check if in overage"})

	ctx, span := pbinit.InitIsOverageRequest(request, subject)
	defer span.End()

	response := a.checkUserOverages(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) CheckUserOveragesHTTPHandler(c echo.Context) error {
	var request qms.IsOverageRequest

	ctx := c.Request().Context()

	request = qms.IsOverageRequest{
		Username:     c.Param("username"),
		ResourceName: c.Param("resource_name"),
	}

	response := a.checkUserOverages(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
