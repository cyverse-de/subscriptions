package app

import (
	"context"
	"net/http"

	"github.com/cyverse-de/go-mod/pbinit"
	"github.com/cyverse-de/p/go/ptypes"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/subscriptions/db"
	"github.com/cyverse-de/subscriptions/errors"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func (a *App) getUsages(ctx context.Context, request *qms.GetUsages) *qms.UsageList {
	response := pbinit.NewUsageList()

	username, err := a.FixUsername(request.Username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	d := db.New(a.db)

	subscription, err := d.GetActiveSubscription(ctx, username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	usages, err := d.SubscriptionUsages(ctx, subscription.ID)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	response.Usages = []*qms.Usage{}
	for _, usage := range usages {
		response.Usages = append(response.Usages, &qms.Usage{
			Uuid:           usage.ID,
			Usage:          usage.Usage,
			SubscriptionId: subscription.ID,
			ResourceType: &qms.ResourceType{
				Uuid:       usage.ResourceType.ID,
				Name:       usage.ResourceType.Name,
				Unit:       usage.ResourceType.Unit,
				Consumable: usage.ResourceType.Consumable,
			},
			CreatedAt:      ptypes.New(usage.CreatedAt),
			CreatedBy:      usage.CreatedBy,
			LastModifiedBy: usage.LastModifiedBy,
			LastModifiedAt: ptypes.New(usage.LastModifiedAt),
		})
	}

	return response
}

func (a *App) GetUsagesHandler(subject, reply string, request *qms.GetUsages) {
	var err error

	log := log.WithFields(logrus.Fields{"context": "getting usages"})

	ctx, span := pbinit.InitGetUsages(request, subject)
	defer span.End()

	response := a.getUsages(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) GetUsagesHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &qms.GetUsages{
		Username: c.Param("username"),
	}

	response := a.getUsages(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) addUsage(ctx context.Context, request *qms.AddUsage) *qms.UsageResponse {
	var (
		err   error
		usage db.Usage
	)

	response := pbinit.NewUsageResponse()
	username, err := a.FixUsername(request.Username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	d := db.New(a.db)

	// Do most of the work in a transaction.
	tx, err := d.Begin()
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	err = tx.Wrap(func() error {
		// Get the user's current active subscription.
		subscription, err := d.GetActiveSubscription(ctx, username, db.WithTX(tx))
		if err != nil {
			return err
		}

		// Validate update type.
		if _, err = d.GetOperationID(ctx, request.UpdateType, db.WithTX(tx)); err != nil {
			return err
		}

		// Get the resource type ID.
		resourceType, err := d.GetResourceTypeByName(ctx, request.ResourceName, db.WithTX(tx))
		if err != nil {
			return err
		}

		// Prepare the usage struct.
		usage = db.Usage{
			Usage:          request.UsageValue,
			SubscriptionID: subscription.ID,
			ResourceType:   *resourceType,
		}

		// Calculate the updated usage.
		if err = d.CalculateUsage(ctx, request.UpdateType, &usage, db.WithTX(tx)); err != nil {
			return err
		}

		// Get the usages.
		u, _, err := d.GetCurrentUsage(ctx, resourceType.ID, subscription.ID, db.WithTX(tx))
		if err != nil {
			return err
		}

		// Return the current usage.
		response.Usage = &qms.Usage{
			Usage:          u,
			SubscriptionId: subscription.ID,
			ResourceType: &qms.ResourceType{
				Uuid:       resourceType.ID,
				Name:       resourceType.Name,
				Unit:       resourceType.Unit,
				Consumable: resourceType.Consumable,
			},
		}

		return nil
	})
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	return response
}

func (a *App) AddUsageHandler(subject, reply string, request *qms.AddUsage) {
	var err error

	ctx, span := pbinit.InitAddUsage(request, subject)
	defer span.End()

	response := a.addUsage(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) AddUsageHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddUsage
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	request.Username = c.Param("username")

	response := a.addUsage(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
