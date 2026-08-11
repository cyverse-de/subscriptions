package app

import (
	"context"
	"net/http"

	"errors"

	serrors "github.com/cyverse-de/subscriptions/errors"
	"github.com/labstack/echo/v4"

	qmsinit "github.com/cyverse-de/go-mod/pbinit/qms"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/p/go/requests"
	"github.com/cyverse-de/subscriptions/db"
)

func (a *App) addAddon(ctx context.Context, request *qms.AddAddonRequest) *qms.AddonResponse {
	var newAddon *db.Addon
	d := db.New(a.db)
	response := qmsinit.NewAddonResponse()

	// The lax JSON decoder accepts a request with no addon object; guard before
	// dereferencing it.
	if request.Addon == nil {
		response.Error = serrors.NatsError(ctx, serrors.ErrInvalidRequestBody)
		return response
	}

	// Validate the incoming request.
	requestedAddon := db.NewAddonFromQMS(request.Addon)
	if err := requestedAddon.Validate(); err != nil {
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(err))
		return response
	}
	if err := requestedAddon.ValidateAddonRateUniqueness(); err != nil {
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(err))
		return response
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

	// The lax JSON decoder accepts a request with no addon object; guard before
	// dereferencing it.
	if request.Addon == nil {
		response.Error = serrors.NatsError(ctx, serrors.ErrInvalidRequestBody)
		return response
	}

	if request.Addon.Uuid == "" {
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(errors.New("uuid must be set in the request")))
		return response
	}

	if err := requireUUID(request.Addon.Uuid, "uuid"); err != nil {
		response.Error = serrors.NatsError(ctx, err)
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

	if request.Addon == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "request body must include an addon",
		})
	}

	addonID, err := uuidParam(c, "uuid")
	if err != nil {
		return err
	}
	request.Addon.Uuid = addonID

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

func (a *App) DeleteAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	addonID, err := uuidParam(c, "uuid")
	if err != nil {
		return err
	}

	request := requests.ByUUID{
		Uuid: addonID,
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

func (a *App) ListSubscriptionAddonsHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	subscriptionID, err := uuidParam(c, "uuid")
	if err != nil {
		return err
	}

	request := &requests.ByUUID{
		Uuid: subscriptionID,
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

func (a *App) GetSubscriptionAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()
	logDeprecatedRoute(c, "GET /subscription-addons/{uuid}")

	addonID, err := uuidParam(c, "addon_uuid")
	if err != nil {
		return err
	}

	request := &requests.ByUUID{
		Uuid: addonID,
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
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(errors.New("parent_uuid must be set to the subscription UUID")))
		return response
	}

	addonID := request.ChildUuid
	if addonID == "" {
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(errors.New("child_id must be set to the add-on UUID")))
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

func (a *App) AddSubscriptionAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	subscriptionID, err := uuidParam(c, "sub_uuid")
	if err != nil {
		return err
	}
	addonID, err := uuidParam(c, "addon_uuid")
	if err != nil {
		return err
	}

	request := &requests.AssociateByUUIDs{
		ParentUuid: subscriptionID,
		ChildUuid:  addonID,
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
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(errors.New("subscription addon-on UUID must be set")))
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

func (a *App) DeleteSubscriptionAddonHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()
	logDeprecatedRoute(c, "DELETE /subscription-addons/{uuid}")

	addonID, err := uuidParam(c, "addon_uuid")
	if err != nil {
		return err
	}

	request := &requests.ByUUID{
		Uuid: addonID,
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

	// The lax JSON decoder accepts a request with no subscription_addon object;
	// guard before dereferencing it.
	if request.SubscriptionAddon == nil {
		response.Error = serrors.NatsError(ctx, serrors.ErrInvalidRequestBody)
		return response
	}

	if request.SubscriptionAddon.Uuid == "" {
		response.Error = serrors.NatsError(ctx, serrors.AsBadRequest(errors.New("uuid must be set in the request")))
		return response
	}

	if err := requireUUID(request.SubscriptionAddon.Uuid, "uuid"); err != nil {
		response.Error = serrors.NatsError(ctx, err)
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

func (a *App) UpdateSubscriptionAddonHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.UpdateSubscriptionAddonRequest
	)

	ctx := c.Request().Context()
	logDeprecatedRoute(c, "POST /subscription-addons/{uuid}")

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

// The handlers below serve /subscription-addons/{uuid}, which names what these
// operations actually take: a subscription add-on's own UUID.
//
// The nested routes they replace are misdescribed by their own paths. Their
// :addon_uuid segment is a subscription add-on's UUID rather than an add-on's,
// and their :sub_uuid segment is ignored, so terrain passes the same value
// twice. Making the nested paths authoritative instead is not possible:
// subscription_addons has no unique constraint on (subscription_id, addon_id),
// so a subscription and an add-on together do not identify one row.
//
// The nested routes still work and still carry the callers. Once terrain moves
// over -- the deprecation warnings each one logs are how you can tell -- they
// can be deleted, and the ByID suffix here goes with them.

func (a *App) GetSubscriptionAddonByIDHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	subAddonID, err := uuidParam(c, "uuid")
	if err != nil {
		return err
	}

	response := a.getSubscriptionAddon(ctx, &requests.ByUUID{Uuid: subAddonID})

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) DeleteSubscriptionAddonByIDHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	subAddonID, err := uuidParam(c, "uuid")
	if err != nil {
		return err
	}

	response := a.deleteSubscriptionAddon(ctx, &requests.ByUUID{Uuid: subAddonID})

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) UpdateSubscriptionAddonByIDHTTPHandler(c echo.Context) error {
	var request qms.UpdateSubscriptionAddonRequest

	ctx := c.Request().Context()

	subAddonID, err := uuidParam(c, "uuid")
	if err != nil {
		return err
	}

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	// The path identifies the row on this route, so a body that names a
	// different one does not get to win. A body with no subscription add-on at
	// all still falls through to the guard in updateSubscriptionAddon.
	if request.SubscriptionAddon != nil {
		request.SubscriptionAddon.Uuid = subAddonID
	}

	response := a.updateSubscriptionAddon(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
