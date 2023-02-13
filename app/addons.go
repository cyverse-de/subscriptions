package app

import (
	"context"

	"errors"

	serrors "github.com/cyverse-de/subscriptions/errors"
	"github.com/sirupsen/logrus"

	qmsinit "github.com/cyverse-de/go-mod/pbinit/qms"
	reqinit "github.com/cyverse-de/go-mod/pbinit/requests"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/p/go/requests"
	"github.com/cyverse-de/subscriptions/db"
)

func (a *App) sendAddonResponseError(reply string, log *logrus.Entry) func(context.Context, *qms.AddonResponse, error) {
	return func(ctx context.Context, response *qms.AddonResponse, err error) {
		log.Error(err)
		response.Error = serrors.NatsError(ctx, err)
		if err = a.client.Respond(ctx, reply, response); err != nil {
			log.Error(err)
		}
	}
}

func (a *App) sendAddonListResponseError(reply string, log *logrus.Entry) func(context.Context, *qms.AddonListResponse, error) {
	return func(ctx context.Context, response *qms.AddonListResponse, err error) {
		log.Error(err)
		response.Error = serrors.NatsError(ctx, err)
		if err = a.client.Respond(ctx, reply, response); err != nil {
			log.Error(err)
		}
	}
}

func (a *App) sendSubscriptionAddonListResponseError(reply string, log *logrus.Entry) func(context.Context, *qms.SubscriptionAddonListResponse, error) {
	return func(ctx context.Context, response *qms.SubscriptionAddonListResponse, err error) {
		log.Error(err)
		response.Error = serrors.NatsError(ctx, err)
		if err = a.client.Respond(ctx, reply, response); err != nil {
			log.Error(err)
		}
	}
}

func (a *App) sendSubscriptionAddonResponseError(reply string, log *logrus.Entry) func(context.Context, *qms.SubscriptionAddonResponse, error) {
	return func(ctx context.Context, response *qms.SubscriptionAddonResponse, err error) {
		log.Error(err)
		response.Error = serrors.NatsError(ctx, err)
		if err = a.client.Respond(ctx, reply, response); err != nil {
			log.Error(err)
		}
	}
}

func (a *App) AddAddonHandler(subject, reply string, request *qms.AddAddonRequest) {
	var err error

	ctx, span := qmsinit.InitAddAddonRequest(request, subject)
	defer span.End()

	log := log.WithField("context", "adding new available addon")
	response := qmsinit.NewAddonResponse()
	sendError := a.sendAddonResponseError(reply, log)
	d := db.New(a.db)

	reqAddon := request.Addon

	if reqAddon.Name == "" {
		sendError(ctx, response, errors.New("name must be set"))
		return
	}

	if reqAddon.Description == "" {
		sendError(ctx, response, errors.New("descriptions must be set"))
		return
	}

	if reqAddon.DefaultAmount <= 0.0 {
		sendError(ctx, response, errors.New("default_amount must be greater than 0.0"))
		return
	}

	if reqAddon.ResourceType.Name == "" && reqAddon.ResourceType.Uuid == "" {
		sendError(ctx, response, errors.New("resource_type.name or resource_type.uuid must be set"))
		return
	}

	var lookupRT *db.ResourceType

	tx, err := d.Begin()
	if err != nil {
		sendError(ctx, response, err)
		return
	}
	defer tx.Rollback()

	if reqAddon.ResourceType.Name != "" && reqAddon.ResourceType.Uuid == "" {
		lookupRT, err = d.GetResourceTypeByName(ctx, reqAddon.ResourceType.Name, db.WithTX(tx))
		if err != nil {
			sendError(ctx, response, err)
			return
		}
	} else {
		lookupRT, err = d.GetResourceType(ctx, reqAddon.ResourceType.Uuid, db.WithTX(tx))
		if err != nil {
			sendError(ctx, response, err)
			return
		}
	}

	newAddon := db.NewAddonFromQMS(request.Addon)
	newAddon.ResourceType = *lookupRT

	newID, err := d.AddAddon(ctx, newAddon, db.WithTX(tx))
	if err != nil {
		sendError(ctx, response, err)
		return
	}

	if err = tx.Commit(); err != nil {
		sendError(ctx, response, err)
		return
	}

	response.Addon = newAddon.ToQMSType()
	response.Addon.Uuid = newID

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

// ListAddonsHandler lists all of the available add-ons in the system. These are
// the ones that can be applied to a subscription, not the ones that have been
// applied already.
func (a *App) ListAddonsHandler(subject, reply string, request *qms.NoParamsRequest) {
	var err error

	ctx, span := qmsinit.InitNoParamsRequest(request, subject)
	defer span.End()

	log := log.WithField("context", "list addons")
	sendError := a.sendAddonListResponseError(reply, log)
	response := qmsinit.NewAddonListResponse()
	d := db.New(a.db)

	results, err := d.ListAddons(ctx)
	if err != nil {
		sendError(ctx, response, err)
		return
	}

	for _, addon := range results {
		response.Addons = append(response.Addons, addon.ToQMSType())
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) UpdateAddonHandler(subject, reply string, request *qms.UpdateAddonRequest) {
	var err error

	log := log.WithField("context", "update addon")

	response := qmsinit.NewAddonResponse()

	sendError := func(ctx context.Context, response *qms.AddonResponse, err error) {
		log.Error(err)
		response.Error = serrors.NatsError(ctx, err)
		if err = a.client.Respond(ctx, reply, response); err != nil {
			log.Error(err)
		}
	}

	ctx, span := qmsinit.InitUpdateAddonRequest(request, subject)
	defer span.End()

	d := db.New(a.db)

	if request.Addon.Uuid == "" {
		sendError(ctx, response, errors.New("uuid must be set in the request"))
		return
	}

	updateAddon := db.NewUpdateAddonFromQMS(request)

	result, err := d.UpdateAddon(ctx, updateAddon)
	if err != nil {
		sendError(ctx, response, err)
		return
	}

	response.Addon = result.ToQMSType()

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) DeleteAddonHandler(subject, reply string, request *requests.ByUUID) {
	var err error

	log := log.WithField("context", "delete addon")

	response := qmsinit.NewAddonResponse()

	sendError := func(ctx context.Context, response *qms.AddonResponse, err error) {
		log.Error(err)
		response.Error = serrors.NatsError(ctx, err)
		if err = a.client.Respond(ctx, reply, response); err != nil {
			log.Error(err)
		}
	}

	ctx, span := reqinit.InitByUUID(request, subject)
	defer span.End()

	d := db.New(a.db)

	if err = d.DeleteAddon(ctx, request.Uuid); err != nil {
		sendError(ctx, response, err)
		return
	}

	response.Addon = &qms.Addon{
		Uuid: request.Uuid,
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

// ListSubscriptionAddonsHandler lists the add-ons that have been applied to the
// indicated subscription.
func (a *App) ListSubscriptionAddonsHandler(subject, reply string, request *requests.ByUUID) {
	var err error

	ctx, span := reqinit.InitByUUID(request, subject)
	defer span.End()

	log := log.WithField("context", "listing subscription add-ons")
	response := qmsinit.NewSubscriptionAddonListResponse()
	sendError := a.sendSubscriptionAddonListResponseError(reply, log)
	d := db.New(a.db)

	results, err := d.ListSubscriptionAddons(ctx, request.Uuid)
	if err != nil {
		sendError(ctx, response, err)
		return
	}

	for _, addon := range results {
		response.SubscriptionAddons = append(response.SubscriptionAddons, addon.ToQMSType())
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) AddSubscriptionAddonHandler(subject, reply string, request *requests.AssociateByUUIDs) {
	var err error

	ctx, span := reqinit.InitAssociateByUUIDs(request, subject)
	defer span.End()

	log := log.WithField("context", "adding subscription add-on")
	response := qmsinit.NewSubscriptionAddonResponse()
	sendError := a.sendSubscriptionAddonResponseError(reply, log)
	d := db.New(a.db)

	subscriptionID := request.ParentUuid
	if subscriptionID == "" {
		sendError(ctx, response, errors.New("parent_uuid must be set to the subscription UUID"))
		return
	}

	addonID := request.ChildUuid
	if addonID == "" {
		sendError(ctx, response, errors.New("child_id must be set to the add-on UUID"))
		return
	}

	result, err := d.AddSubscriptionAddon(ctx, subscriptionID, addonID)
	if err != nil {
		sendError(ctx, response, err)
		return
	}

	response.SubscriptionAddon = result.ToQMSType()

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}
