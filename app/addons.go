package app

import (
	"context"
	"net/http"

	"errors"

	serrors "github.com/cyverse-de/subscriptions/errors"
	"github.com/labstack/echo/v4"

	qmsinit "github.com/cyverse-de/go-mod/pbinit/qms"
	reqinit "github.com/cyverse-de/go-mod/pbinit/requests"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/p/go/requests"
	"github.com/cyverse-de/subscriptions/db"
)

func (a *App) addAddon(ctx context.Context, request *qms.AddAddonRequest) *qms.AddonResponse {
	var newAddon *db.Addon
	d := db.New(a.db)
	response := qmsinit.NewAddonResponse()

	// Validate the incoming request.
	requestedAddon := db.NewAddonFromQMS(request.Addon)
	if err := requestedAddon.Validate(); err != nil {
		response.Error = serrors.NatsError(ctx, err)
	}
	if err := requestedAddon.ValidateAddonRateUniqueness(); err != nil {
		response.Error = serrors.NatsError(ctx, err)
	}

	// Start a transaction.
	tx, err := d.Begin()
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}
	err = tx.Wrap(func() error {

		// Look up the resource type.
		resourceType, err := d.LookupResoureType(ctx, &requestedAddon.ResourceType, db.WithTX(tx))
		if err != nil {
			return err
		}
		requestedAddon.ResourceType = *resourceType

		// Add the addon to the database.
		addonID, err := d.AddAddon(ctx, requestedAddon, db.WithTX(tx))
		if err != nil {
			return err
		}

		// Retrieve the addon from the database.
		newAddon, err = d.GetAddonByID(ctx, addonID, db.WithTX(tx))
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// Return the inserted addon.
	response.Addon = newAddon.ToQMSType()
	return response
}

func (a *App) AddAddonHandler(subject, reply string, request *qms.AddAddonRequest) {
	var err error

	ctx, span := qmsinit.InitAddAddonRequest(request, subject)
	defer span.End()

	log := log.WithField("context", "adding new available addon")

	response := a.addAddon(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) AddAddonHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddAddonRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "invalid body format",
		})
	}

	response := a.addAddon(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)

}

func (a *App) listAddons(ctx context.Context) *qms.AddonListResponse {
	response := qmsinit.NewAddonListResponse()
	d := db.New(a.db)

	results, err := d.ListAddons(ctx)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	response.Addons = make([]*qms.Addon, 0, len(results))
	for _, addon := range results {
		response.Addons = append(response.Addons, addon.ToQMSType())
	}
	return response
}

// ListAddonsHandler lists all of the available add-ons in the system. These are
// the ones that can be applied to a subscription, not the ones that have been
// applied already.
func (a *App) ListAddonsHandler(subject, reply string, request *qms.NoParamsRequest) {
	var err error

	ctx, span := qmsinit.InitNoParamsRequest(request, subject)
	defer span.End()

	log := log.WithField("context", "list addons")

	response := a.listAddons(ctx)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) ListAddonsHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	response := a.listAddons(ctx)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)

}

func (a *App) updateAddon(ctx context.Context, request *qms.UpdateAddonRequest) *qms.AddonResponse {
	response := qmsinit.NewAddonResponse()
	d := db.New(a.db)

	if request.Addon.Uuid == "" {
		response.Error = serrors.NatsError(ctx, errors.New("uuid must be set in the request"))
		return response
	}

	updateAddon := db.NewUpdateAddonFromQMS(request)

	tx, err := d.Begin()
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}
	err = tx.Wrap(func() error {
		err := d.UpdateAddon(ctx, updateAddon, db.WithTX(tx))
		if err != nil {
			return err
		}

		result, err := d.GetAddonByID(ctx, updateAddon.ID, db.WithTX(tx))
		if err != nil {
			return err
		}
		response.Addon = result.ToQMSType()

		return nil
	})
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
	}
	return response
}

func (a *App) UpdateAddonHandler(subject, reply string, request *qms.UpdateAddonRequest) {
	var err error

	log := log.WithField("context", "update addon")

	ctx, span := qmsinit.InitUpdateAddonRequest(request, subject)
	defer span.End()

	response := a.updateAddon(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) UpdateAddonHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.UpdateAddonRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	request.Addon.Uuid = c.Param("uuid")

	response := a.updateAddon(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)

}

func (a *App) deleteAddon(ctx context.Context, request *requests.ByUUID) *qms.AddonResponse {
	response := qmsinit.NewAddonResponse()

	d := db.New(a.db)

	subAddons, err := d.ListSubscriptionAddonsByAddonID(ctx, request.Uuid)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	if len(subAddons) > 0 {
		response.Error = serrors.NatsError(ctx, serrors.ErrSubscriptionAddonsExist)
		return response
	}

	if err = d.DeleteAddon(ctx, request.Uuid); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	response.Addon = &qms.Addon{
		Uuid:       request.Uuid,
		AddonRates: []*qms.AddonRate{},
	}

	return response
}

func (a *App) DeleteAddonHandler(subject, reply string, request *requests.ByUUID) {
	var err error

	log := log.WithField("context", "delete addon")

	ctx, span := reqinit.InitByUUID(request, subject)
	defer span.End()

	response := a.deleteAddon(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) DeleteAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := requests.ByUUID{
		Uuid: c.Param("uuid"),
	}

	response := a.deleteAddon(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) listSubscriptionAddons(ctx context.Context, request *requests.ByUUID) *qms.SubscriptionAddonListResponse {
	response := qmsinit.NewSubscriptionAddonListResponse()

	d := db.New(a.db)
	tx, err := d.Begin()
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	err = tx.Wrap(func() error {
		results, err := d.ListSubscriptionAddons(ctx, request.Uuid, db.WithTX(tx))
		if err != nil {
			return err
		}

		response.SubscriptionAddons = make([]*qms.SubscriptionAddon, 0, len(results))
		for _, addon := range results {
			response.SubscriptionAddons = append(response.SubscriptionAddons, addon.ToQMSType())
		}

		return nil
	})
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
	}

	return response
}

// ListSubscriptionAddonsHandler lists the add-ons that have been applied to the
// indicated subscription.
func (a *App) ListSubscriptionAddonsHandler(subject, reply string, request *requests.ByUUID) {
	var err error

	ctx, span := reqinit.InitByUUID(request, subject)
	defer span.End()

	log := log.WithField("context", "listing subscription add-ons")

	response := a.listSubscriptionAddons(ctx, request)
	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) ListSubscriptionAddonsHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &requests.ByUUID{
		Uuid: c.Param("uuid"),
	}

	response := a.listSubscriptionAddons(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) getSubscriptionAddon(ctx context.Context, request *requests.ByUUID) *qms.SubscriptionAddonResponse {
	response := qmsinit.NewSubscriptionAddonResponse()

	d := db.New(a.db)

	subAddon, err := d.GetSubscriptionAddonByID(ctx, request.Uuid)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	response.SubscriptionAddon = subAddon.ToQMSType()

	return response
}

// GetSubscriptionAddonHandler gets a single addon based on it's UUID.
func (a *App) GetSubscriptionAddonHandler(subject, reply string, request *requests.ByUUID) {
	var err error

	ctx, span := reqinit.InitByUUID(request, subject)
	defer span.End()

	log := log.WithField("context", "getting subscription add-on")

	response := a.getSubscriptionAddon(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) GetSubscriptionAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &requests.ByUUID{
		Uuid: c.Param("addon_uuid"),
	}

	response := a.getSubscriptionAddon(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) addSubscriptionAddon(ctx context.Context, request *requests.AssociateByUUIDs) *qms.SubscriptionAddonResponse {
	response := qmsinit.NewSubscriptionAddonResponse()
	d := db.New(a.db)

	subscriptionID := request.ParentUuid
	if subscriptionID == "" {
		response.Error = serrors.NatsError(ctx, errors.New("parent_uuid must be set to the subscription UUID"))
		return response
	}

	addonID := request.ChildUuid
	if addonID == "" {
		response.Error = serrors.NatsError(ctx, errors.New("child_id must be set to the add-on UUID"))
		return response
	}

	tx, err := d.Begin()
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}
	defer func() {
		_ = tx.Rollback()
	}()

	subAddon, err := d.AddSubscriptionAddon(ctx, subscriptionID, addonID, db.WithTXRollbackCommit(tx, false, false))
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	quotaValue, _, err := d.GetCurrentQuota(
		ctx,
		subAddon.Addon.ResourceType.ID,
		subscriptionID,
		db.WithTXRollbackCommit(tx, false, false),
	)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	quotaValue = quotaValue + subAddon.Amount
	if err = d.UpsertQuota(
		ctx,
		quotaValue,
		subAddon.Addon.ResourceType.ID,
		subscriptionID,
		db.WithTXRollbackCommit(tx, false, false),
	); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	if err = tx.Commit(); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	response.SubscriptionAddon = subAddon.ToQMSType()
	return response
}

func (a *App) AddSubscriptionAddonHandler(subject, reply string, request *requests.AssociateByUUIDs) {
	var err error

	ctx, span := reqinit.InitAssociateByUUIDs(request, subject)
	defer span.End()

	log := log.WithField("context", "adding subscription add-on")

	response := a.addSubscriptionAddon(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) AddSubscriptionAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &requests.AssociateByUUIDs{
		ParentUuid: c.Param("sub_uuid"),
		ChildUuid:  c.Param("addon_uuid"),
	}

	response := a.addSubscriptionAddon(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) deleteSubscriptionAddon(ctx context.Context, request *requests.ByUUID) *qms.SubscriptionAddonResponse {
	response := qmsinit.NewSubscriptionAddonResponse()
	d := db.New(a.db)

	// Get the subscription add-on ID out of the request.
	subAddonID := request.Uuid
	if subAddonID == "" {
		response.Error = serrors.NatsError(ctx, errors.New("subscription addon-on UUID must be set"))
		return response
	}

	/// Start the database transaction.
	tx, err := d.Begin()
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// Get the subscription add-on details from the database. Needed to modify
	// the quota value.
	subAddon, err := d.GetSubscriptionAddonByID(ctx, subAddonID, db.WithTX(tx))
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// Get the current quota value.
	quotaValue, _, err := d.GetCurrentQuota(
		ctx,
		subAddon.Addon.ResourceType.ID,
		subAddon.SubscriptionID,
		db.WithTXRollbackCommit(tx, false, false),
	)
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// Update the quota value by subtracting the amount configured in the
	// subscription add-on. We don't want the available add-on value, we want
	// the subscription add-on value, which may have been modified from the
	// available add-on value.
	quotaValue = quotaValue - subAddon.Amount
	if err = d.UpsertQuota(
		ctx,
		quotaValue,
		subAddon.Addon.ResourceType.ID,
		subAddon.SubscriptionID,
		db.WithTXRollbackCommit(tx, false, false),
	); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// Delete the subscription add-on.
	if err = d.DeleteSubscriptionAddon(ctx, subAddonID, db.WithTX(tx)); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// Commit all of the changes.
	if err = tx.Commit(); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	// Return the response.
	response.SubscriptionAddon = subAddon.ToQMSType()

	return response
}

func (a *App) DeleteSubscriptionAddonHandler(subject, reply string, request *requests.ByUUID) {
	var err error

	ctx, span := reqinit.InitByUUID(request, subject)
	defer span.End()

	log := log.WithField("context", "deleting subscription add-ons")

	response := a.deleteSubscriptionAddon(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) DeleteSubscriptionAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &requests.ByUUID{
		Uuid: c.Param("addon_uuid"),
	}

	response := a.deleteSubscriptionAddon(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) updateSubscriptionAddon(ctx context.Context, request *qms.UpdateSubscriptionAddonRequest) *qms.SubscriptionAddonResponse {
	response := qmsinit.NewSubscriptionAddonResponse()

	d := db.New(a.db)

	if request.SubscriptionAddon.Uuid == "" {
		response.Error = serrors.NatsError(ctx, errors.New("uuid must be set in the request"))
		return response
	}

	subAddonID := request.SubscriptionAddon.Uuid
	updateSubAddon := db.NewUpdateSubscriptionAddonFromQMS(request)

	/// Start the database transaction.
	tx, err := d.Begin()
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if updateSubAddon.UpdateAmount {
		// Get the pre-update subscription add-on details from the database. Needed
		// to modify the quota value.
		preUpdateSubAddon, err := d.GetSubscriptionAddonByID(ctx, subAddonID, db.WithTX(tx))
		if err != nil {
			response.Error = serrors.NatsError(ctx, err)
			return response
		}

		// Get the current quota value.
		quotaValue, _, err := d.GetCurrentQuota(
			ctx,
			preUpdateSubAddon.Addon.ResourceType.ID,
			preUpdateSubAddon.SubscriptionID,
			db.WithTXRollbackCommit(tx, false, false),
		)
		if err != nil {
			response.Error = serrors.NatsError(ctx, err)
			return response
		}

		// First, remove the pre-update subscription add-on value from the quota
		// value.
		quotaValue = quotaValue - preUpdateSubAddon.Amount

		// Next, add the new value for the subscription add-on.
		quotaValue = quotaValue + updateSubAddon.Amount

		// Now update the quota value
		if err = d.UpsertQuota(
			ctx,
			quotaValue,
			preUpdateSubAddon.Addon.ResourceType.ID,
			preUpdateSubAddon.SubscriptionID,
			db.WithTXRollbackCommit(tx, false, false),
		); err != nil {
			response.Error = serrors.NatsError(ctx, err)
			return response
		}
	}

	result, err := d.UpdateSubscriptionAddon(ctx, updateSubAddon, db.WithTXRollbackCommit(tx, false, false))
	if err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	if err = tx.Commit(); err != nil {
		response.Error = serrors.NatsError(ctx, err)
		return response
	}

	response.SubscriptionAddon = result.ToQMSType()

	return response
}

func (a *App) UpdateSubscriptionAddonHandler(subject, reply string, request *qms.UpdateSubscriptionAddonRequest) {
	var err error

	ctx, span := qmsinit.InitUpdateSubscriptionAddonRequest(request, subject)
	defer span.End()

	log := log.WithField("context", "update subscription addon")

	response := a.updateSubscriptionAddon(ctx, request)

	if response.Error != nil {
		log.Debug(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) UpdateSubscriptionAddonHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.UpdateSubscriptionAddonRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	response := a.updateSubscriptionAddon(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
