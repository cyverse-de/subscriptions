package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cyverse-de/go-mod/pbinit"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/subscriptions/db"
	"github.com/cyverse-de/subscriptions/errors"
	"github.com/cyverse-de/subscriptions/utils"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

func (a *App) addUser(ctx context.Context, request *qms.AddUserRequest) *qms.AddUserResponse {
	response := pbinit.NewQMSAddUserResponse()
	username, err := a.FixUsername(request.Username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	d := db.New(a.db)

	opts, err := utils.OptsForValues(request.Paid, request.Periods, request.EndDate)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	tx, err := d.Begin()
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	defer func() {
		_ = tx.Rollback()
	}()

	// extract information about the subscription from the request
	planName := request.PlanName
	if planName == "" {
		response.Error = errors.NatsError(ctx, errors.AsBadRequest(
			errors.New("no plan name provided in the request body"),
		))
		return response
	}

	plan, err := d.GetPlanByName(ctx, planName, db.WithTX(tx))
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	// A plan that doesn't exist comes back as a nil plan with no error, and
	// every use of it below dereferences it.
	if plan == nil {
		response.Error = errors.NatsError(ctx, errors.AsBadRequest(
			fmt.Errorf("subscription plan does not exist: %s", planName),
		))
		return response
	}

	// look for an existing user.
	userExists, err := d.UserExists(ctx, username, db.WithTX(tx))
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	var userID string

	// add the user.
	if !userExists {
		userID, err = d.AddUser(ctx, username, db.WithTX(tx))
		if err != nil {
			response.Error = errors.NatsError(ctx, err)
			return response
		}
	} else {
		userID, err = d.GetUserID(ctx, username, db.WithTX(tx))
		if err != nil {
			response.Error = errors.NatsError(ctx, err)
			return response
		}
	}

	// Create a new subscription if the caller requested it.
	createSubscription := request.Force

	// Also create a new subscription if the user doesn't have one yet.
	if !createSubscription {
		hasPlan, err := d.UserHasActivePlan(ctx, username, db.WithTX(tx))
		if err != nil {
			response.Error = errors.NatsError(ctx, err)
			return response
		}
		createSubscription = !hasPlan
	}

	// Also create a new subscription if the user's current subscription plan doesn't match the requested one.
	if !createSubscription {
		onPlan, err := d.UserOnPlan(ctx, username, plan.Name, db.WithTX(tx))
		if err != nil {
			response.Error = errors.NatsError(ctx, err)
			return response
		}
		createSubscription = !onPlan
	}

	// Create the subscription if we're supposed to.
	if createSubscription {
		if _, err = d.SetActiveSubscription(ctx, userID, plan, opts, db.WithTX(tx)); err != nil {
			response.Error = errors.NatsError(ctx, err)
			return response
		}
		// The only record of who was put on which plan, and whether it was paid:
		// the request carries them in its body, so the route alone doesn't say.
		log.WithFields(logrus.Fields{
			"user":     username,
			"plan":     plan.Name,
			"paid":     opts.Paid,
			"periods":  opts.Periods,
			"end_date": opts.EndDate,
		}).Info("subscribed the user to a plan")
	}

	// Commit all of the changes
	if err = tx.Commit(); err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	response.PlanName = plan.Name
	response.PlanUuid = plan.ID
	response.Username = username
	response.Uuid = userID

	return response
}

func (a *App) AddUserHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddUserRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	// The username comes from the body; this route declares no path parameters.
	// It used to be overwritten with c.Param("username"), which is always empty
	// here, so every call subscribed a blank username instead of the requested
	// one.
	if request.Username == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "no username provided in the request body",
		})
	}

	response := a.addUser(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
