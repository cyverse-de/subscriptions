package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cyverse-de/go-mod/pbinit"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/subscriptions/db"
	"github.com/cyverse-de/subscriptions/errors"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func (a *App) GetUserSummary(ctx context.Context, username string) (*qms.Subscription, error) {
	// Set up the log context.
	log := log.WithFields(
		logrus.Fields{
			"context": "user summary",
			"user":    username,
		},
	)

	// Get the user summary.
	d := db.New(a.db)

	var subscription *db.Subscription
	tx, err := d.Begin()
	if err != nil {
		return nil, err
	}
	err = tx.Wrap(func() error {
		log.Debugf("before getting the active user plan: %s", username)

		subscription, err = d.GetActiveSubscription(ctx, username, db.WithTX(tx))
		if err != nil {
			log.Errorf("unable to get the active user plan: %s", err)
			return err
		}
		log.Debugf("after getting the active user plan: %s", username)

		if subscription == nil || subscription.ID == "" {
			user, err := d.EnsureUser(ctx, username, db.WithTX(tx))
			if err != nil {
				log.Errorf("unable to ensure that the user exists in the database: %s", err)
				return err
			}

			plan, err := d.GetPlanByName(ctx, db.DefaultPlanName, db.WithTX(tx))
			if err != nil {
				log.Errorf("unable to look up the default plan: %s", err)
				return err
			}

			opts := db.DefaultSubscriptionOptions()
			subscriptionID, err := d.SetActiveSubscription(ctx, user.ID, plan, opts, db.WithTX(tx))
			if err != nil {
				log.Errorf("unable to subscribe the user to the default plan: %s", err)
				return err
			}

			subscription, err = d.GetSubscriptionByID(ctx, subscriptionID, db.WithTX(tx))
			if err != nil {
				log.Errorf("unable to look up the new user plan: %s", err)
				return err
			}
			if subscription == nil {
				err = fmt.Errorf("the newly inserted user plan could not be found")
				log.Error(err)
				return err
			}
		}

		log.Debug("before getting the user plan details")
		err = d.LoadSubscriptionDetails(ctx, subscription, db.WithTX(tx))
		if err != nil {
			return err
		}
		log.Debug("affter getting the user plan details")

		return nil
	})
	if err != nil {
		return nil, err
	}

	return subscription.ToQMSSubscription(), nil
}

func (a *App) getUserSummary(ctx context.Context, request *qms.RequestByUsername) *qms.SubscriptionResponse {
	response := pbinit.NewSubscriptionResponse()

	username, err := a.FixUsername(request.Username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	subscription, err := a.GetUserSummary(ctx, username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	response.Subscription = subscription

	return response
}

func (a *App) GetUserSummaryHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &qms.RequestByUsername{
		Username: c.Param("user"),
	}

	response := a.getUserSummary(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
