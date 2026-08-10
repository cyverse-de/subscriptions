package controllers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/httpmodel"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model/timestamp"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/query"
	"github.com/doug-martin/goqu/v9"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

const (
	UpdateTypeSet = "SET"
	UpdateTypeAdd = "ADD"
)

// swagger:route GET /v1/users users listUsers
//
// List Users
//
// Lists the users registered in the qms database.
//
// responses:
//   200: userListing
//   500: internalServerErrorResponse

// GetAllUsers lists the users that are currently defined in the database.
func (s Server) GetAllUsers(ctx echo.Context) error {
	context := ctx.Request().Context()

	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		data, err := qmsdb.ListUsers(context, tx)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		return ctx.JSON(http.StatusOK, model.SuccessResponse(data, http.StatusOK))
	})
}

type Result struct {
	ID             *string
	UserName       string
	ResourceTypeID *string
}

// GetSubscriptionDetails returns information about the currently active plan for the user.
func (s Server) GetSubscriptionDetails(ctx echo.Context) error {
	log := log.WithFields(logrus.Fields{"context": "getting active user plan"})

	context := ctx.Request().Context()

	username := strings.TrimSuffix(ctx.Param("username"), s.UsernameSuffix)
	if username == "" {
		return model.Error(ctx, "invalid username", http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{"user": username})

	// Start a transaction.
	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		var err error

		// Look up or insert the user.
		user, err := qmsdb.GetUser(context, tx, username)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debugf("found user %s in db", user.Username)

		// Look up or create the user plan.
		subscription, err := qmsdb.GetActiveSubscriptionDetails(context, tx, user.Username)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		log.Debugf("user plan name is %s", subscription.Plan.Name)

		// Return the user plan.
		return model.Success(ctx, subscription, http.StatusOK)
	})
}

// swagger:route POST /v1/users/:username/plan/:resource-type/quota users updateCurrentSubscriptionQuota
//
// Update Current Subscription Plan Quota
//
// Updates the current quota for the given username and resource type. If the user doesn't have an active
// subscription then a new subscription for the default subscription plan type will be created.
//
// responses:
//   200: subscriptionsResponse
//   400: badRequestResponse
//   500: internalServerErrorResponse

// UpdateCurrentSubscriptionQuota is the handler for updating the quota associated with a user's current
// subscription plan.
func (s Server) UpdateCurrentSubscriptionQuota(c echo.Context) error {
	log := log.WithField("context", "updating a current subscription quota")
	ctx := c.Request().Context()

	// Extract the username from the request.
	username := strings.TrimSuffix(c.Param("username"), s.UsernameSuffix)
	if username == "" {
		msg := fmt.Sprintf("invalid username provided in request: '%s'", c.Param("username"))
		log.Error(msg)
		return model.Error(c, msg, http.StatusBadRequest)
	}
	log = log.WithField("user", username)

	// Extract the resource type name from the request.
	resourceTypeName := c.Param("resource-type")
	if resourceTypeName == "" {
		msg := "no resource type name provided in request"
		log.Error(msg)
		return model.Error(c, msg, http.StatusBadRequest)
	}
	log = log.WithField("resource-type", resourceTypeName)

	// Parse the request body.
	var body httpmodel.QuotaValue
	err := c.Bind(&body)
	if err != nil {
		msg := fmt.Sprintf("invalid request body: %s", err.Error())
		log.Error(msg)
		return model.Error(c, msg, http.StatusBadRequest)
	}
	if err = c.Validate(&body); err != nil {
		msg := fmt.Sprintf("invalid request body: %s", err.Error())
		log.Error(msg)
		return model.Error(c, msg, http.StatusBadRequest)
	}

	// Start a transaction.
	return s.goquTransaction(ctx, func(tx *goqu.TxDatabase) error {
		// Look up the resource type.
		resourceType, err := qmsdb.GetResourceTypeByName(ctx, tx, resourceTypeName)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("resource type '%s' not found", resourceTypeName)
			log.Error(msg)
			return txError(c, msg, http.StatusBadRequest)
		}
		if err != nil {
			log.Error(err)
			return txError(c, err.Error(), http.StatusInternalServerError)
		}

		// Determine whether or not the user has an active subscription.
		hasActiveSubscription, err := qmsdb.HasActiveSubscription(ctx, tx, username)
		if err != nil {
			log.Error(err)
			return txError(c, err.Error(), http.StatusInternalServerError)
		}

		// Load the user's current subscription, creating a new subscription if necessary.
		subcription, err := qmsdb.GetActiveSubscription(ctx, tx, username)
		if err != nil {
			log.Error(err)
			return txError(c, err.Error(), http.StatusInternalServerError)
		}

		// Insert or update the quota.
		quota := &model.Quota{
			SubscriptionID: subcription.ID,
			Quota:          body.Quota,
			ResourceTypeID: resourceType.ID,
		}
		err = qmsdb.UpsertQuota(ctx, tx, quota)
		if err != nil {
			log.Error(err)
			return txError(c, err.Error(), http.StatusInternalServerError)
		}

		// Load the subscription details.
		details, err := qmsdb.GetSubscriptionDetails(ctx, tx, *subcription.ID)
		if err != nil {
			log.Error(err)
			return txError(c, err.Error(), http.StatusInternalServerError)
		}

		// Return the response.
		responseBody := model.SubscriptionResponseFromSubscription(details, !hasActiveSubscription)
		return model.Success(c, responseBody, http.StatusOK)
	})
}

// AddUser adds a new user to the database. This is a no-op if the user already
// exists.
func (s Server) AddUser(ctx echo.Context) error {
	log := log.WithFields(logrus.Fields{"context": "adding user"})

	context := ctx.Request().Context()

	username := strings.TrimSuffix(ctx.Param("username"), s.UsernameSuffix)
	if username == "" {
		return model.Error(ctx, "invalid username", http.StatusBadRequest)
	}

	log.Debugf("user from request is %s", username)

	log = log.WithFields(logrus.Fields{"user": username})

	// Start a transaction.
	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		var err error

		// Either add the user to the database or look up the existing user
		// information.
		user, err := qmsdb.GetUser(context, tx, username)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("found user in the database")

		// GetActiveSubscription will automatically subscribe the user to the basic
		// plan if not subscribed already.
		_, err = qmsdb.GetActiveSubscription(context, tx, user.Username)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("ensured that user is subscribed to a plan")

		return model.Success(ctx, "Success", http.StatusOK)
	})
}

// swagger:route PUT /v1/users/{username}/{plan_name} users updateSubscription
//
// # Subscribe a User to a New Plan
//
// Creates a new subscription for the user with the given username.
//
// Responses:
//   200: subscription
//   400: badRequestResponse
//   500: internalServerErrorResponse

// UpdateSubscription is the handler for the PUT /v1/users/{username}/{plan_name} endpoint.
func (s Server) UpdateSubscription(ctx echo.Context) error {
	log := log.WithFields(logrus.Fields{"context": "updating user plan"})

	context := ctx.Request().Context()

	planName := ctx.Param("plan_name")
	if planName == "" {
		return model.Error(ctx, "invalid plan name", http.StatusBadRequest)
	}
	log.Debugf("plan name from request is %s", planName)

	paid, err := query.ValidateBooleanQueryParam(ctx, "paid", nil)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	log.Debugf("paid flag from request is %t", paid)

	username := strings.TrimSuffix(ctx.Param("username"), s.UsernameSuffix)
	if username == "" {
		return model.Error(ctx, "invalid username", http.StatusBadRequest)
	}
	log.Debugf("user name from request is %s", username)

	var defaultPeriods int32 = 1
	periods, err := query.ValidateIntQueryParam(ctx, "periods", &defaultPeriods, "gte=0")
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	log.Debugf("periods from request is %d", periods)

	defaultStartDate := time.Now()
	startDate, err := query.ValidateDateQueryParam(ctx, "start-date", &defaultStartDate)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	log.Debugf("start date from request is %s", startDate)

	defaultEndDate := startDate.AddDate(int(periods), 0, 0)
	endDate, err := query.ValidateDateQueryParam(ctx, "end-date", &defaultEndDate)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	if !endDate.After(time.Now()) {
		return model.Error(ctx, "end date must be in the future", http.StatusBadRequest)
	}
	log.Debugf("end date from request is %s", endDate)

	if !startDate.Before(endDate) {
		return model.Error(ctx, "the start date must precede the end date", http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{
		"user":       username,
		"plan":       planName,
		"paid":       paid,
		"start-date": startDate,
		"end-date":   endDate,
	})

	// Start a transaction.
	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		var err error

		// Either add the user to the database or look up the existing user information.
		user, err := qmsdb.GetUser(context, tx, username)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		log.Debug("found user in the database")

		// Verify that a plan with the given name exists.
		plan, err := qmsdb.GetPlan(context, tx, planName)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("plan name `%s` not found", planName)
			return txError(ctx, msg, http.StatusBadRequest)
		}
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		log.Debug("verified that plan exists in database")

		// Deactivate conflicting subscriptions for the user.
		err = qmsdb.DeactivateSubscriptions(context, tx, *user.ID, startDate, endDate)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		log.Debug("deactivated conflicting subscriptions for the user")

		// Define the subscription options.
		startTimestamp := timestamp.Timestamp(startDate)
		endTimestamp := timestamp.Timestamp(endDate)
		opts := &model.SubscriptionOptions{
			Paid:      &paid,
			Periods:   &periods,
			StartDate: &startTimestamp,
			EndDate:   &endTimestamp,
		}

		// Subscribe the user to the plan.
		subscription, err := qmsdb.SubscribeUserToPlan(context, tx, user, plan, opts)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		log.Debug("finished adding the new subscription")

		// Load the subscription details.
		details, err := qmsdb.GetSubscriptionDetails(context, tx, *subscription.ID)
		if err != nil {
			log.Error(err)
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		// Return the response.
		return model.Success(ctx, details, http.StatusOK)
	})
}

// ListUserSubscriptions is the handler for the GET /v1/subscriptions/:username endpoint.
//
// swagger:route GET /v1/uers/{username}/subscriptions users listUserSubscriptions
//
// # List Subscriptions for a User
//
// Lists existing CyVerse subscriptions for the named user.
//
// Responses:
//
//	200: subscriptionListing
//	400: badRequestResponse
//	500: internalServerErrorResponse
func (s Server) ListUserSubscriptions(ctx echo.Context) error {
	var err error

	// Initialize the context for the endpoint.
	var log = log.WithField("context", "list-user-subscriptions")
	var context = ctx.Request().Context()

	// Extract the path parameters.
	username := strings.TrimSuffix(ctx.Param("username"), s.UsernameSuffix)
	if username == "" {
		return model.Error(ctx, "invalid username", http.StatusBadRequest)
	}
	log.Debugf("user name from request is %s", username)
	log = log.WithField("username", username)

	// Extract the `include-expired` query parameter.
	includeExpired := false
	includeExpired, err = query.ValidateBooleanQueryParam(ctx, "include-expired", &includeExpired)
	if err != nil {
		log.Error(err)
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	log = log.WithField("include-expired", includeExpired)

	// Extract the `cutoff` query parameter.
	cutoff := time.Now()
	cutoff, err = query.ValidateDateQueryParam(ctx, "cutoff", &cutoff)
	if err != nil {
		log.Error(err)
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	log = log.WithField("cutoff", cutoff)

	log.Infof("obtaining the listing")

	// Obtain the listing.
	var subscriptions []*model.Subscription
	var count int64
	err = s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		subscriptions, count, err = qmsdb.ListSubscriptionsForUser(context, tx, username, includeExpired, cutoff)
		return err
	})
	if err != nil {
		log.Error(err)
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Build the result.
	return model.Success(
		ctx,
		&model.SubscriptionListing{
			Subscriptions: subscriptions,
			Total:         count,
		},
		http.StatusOK,
	)
}
