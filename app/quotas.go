package app

import (
	"context"
	"net/http"

	"github.com/cyverse-de/go-mod/pbinit"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/subscriptions/db"
	"github.com/cyverse-de/subscriptions/errors"
	"github.com/labstack/echo/v4"
)

func (a *App) addQuota(ctx context.Context, request *qms.AddQuotaRequest) *qms.QuotaResponse {
	var err error
	response := pbinit.NewQuotaResponse()

	subscriptionID := request.Quota.SubscriptionId

	d := db.New(a.db)
	tx, err := d.Begin()
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	err = tx.Wrap(func() error {
		var err error

		// Store the quota in the database, overwriting the old quota if one exists for the resource type.
		err = d.UpsertQuota(
			ctx,
			float64(request.Quota.Quota),
			request.Quota.ResourceType.Uuid,
			subscriptionID,
			db.WithTX(tx),
		)
		if err != nil {
			return err
		}

		// Load the quota from the database.
		quota, err := d.LoadQuotaDetails(ctx,
			request.Quota.ResourceType.Uuid,
			subscriptionID,
			db.WithTX(tx),
		)
		if err != nil {
			return err
		}
		if quota == nil {
			return errors.New("unable to load the quota after saving")
		}

		// Save the quota in the response.
		response.Quota = quota.ToQMSQuota()
		return nil
	})
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
	}

	return response
}

func (a *App) AddQuotaHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddQuotaRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	response := a.addQuota(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
