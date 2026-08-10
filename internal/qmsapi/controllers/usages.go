package controllers

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
	"github.com/labstack/echo/v4"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Usage struct {
	Username     string  `json:"username"`
	ResourceName string  `json:"resource_name"`
	UsageValue   float64 `json:"usage_value"`
	UpdateType   string  `json:"update_type"`
	Metadata     string  `json:"metadata"`
}

var (
	ErrUserNotFound        = errors.New("user name not found")
	ErrInvalidUsername     = errors.New("invalid username")
	ErrInvalidResourceName = errors.New("invalid resource name")
	ErrInvalidUsageValue   = errors.New("invalid usage value")
	ErrInvalidUpdateType   = errors.New("invalid update type")
)

// httpStatusCode maps a sentinel error to a status. It matches with errors.Is
// rather than equality so that a sentinel can be wrapped with the offending
// value and still be classified as client input rather than a server fault.
func httpStatusCode(err error) int {
	switch {
	case errors.Is(err, ErrUserNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrInvalidUsername),
		errors.Is(err, ErrInvalidResourceName),
		errors.Is(err, ErrInvalidUsageValue),
		errors.Is(err, ErrInvalidUpdateType):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

func (s Server) addUsage(ctx context.Context, usage *Usage) error {
	username := strings.TrimSuffix(usage.Username, s.UsernameSuffix)
	if username == "" {
		return ErrInvalidUsername
	}

	if usage.ResourceName == "" {
		return ErrInvalidResourceName
	}

	if usage.UsageValue < 0 {
		return ErrInvalidUsageValue
	}

	if usage.UpdateType == "" {
		return ErrInvalidUpdateType
	}

	log.Debug("validated usage information")

	log := log.WithFields(logrus.Fields{
		"user":       username,
		"resource":   usage.ResourceName,
		"updateType": usage.UpdateType,
		"value":      usage.UsageValue,
	})

	return s.goquTransaction(func(tx *goqu.TxDatabase) error {
		// Look up the currently active user plan, adding a default plan if one doesn't exist already.
		subscription, err := qmsdb.GetActiveSubscriptionDetails(ctx, tx, username)
		if err != nil {
			return err
		}

		log.Debugf("active plan is %s", subscription.Plan.Name)

		// Look up the resource type.
		resourceType, err := qmsdb.GetResourceTypeByName(ctx, tx, usage.ResourceName)
		if errors.Is(err, qmsdb.ErrNotFound) {
			return fmt.Errorf("resource type '%s' does not exist", usage.ResourceName)
		}
		if err != nil {
			return err
		}
		log.Debug("found resource type in database")

		// Verify that the update operation for the given update type exists. The name has to be an explicit condition:
		// GORM's First only filtered on the primary key, so setting Name on the destination struct matched whichever
		// operation sorted first and recorded ADD for every update.
		updateOperation, err := qmsdb.GetUpdateOperationByName(ctx, tx, usage.UpdateType)
		if errors.Is(err, qmsdb.ErrNotFound) {
			return fmt.Errorf("%w: %s", ErrInvalidUpdateType, usage.UpdateType)
		}
		if err != nil {
			return err
		}
		log.Debug("verified update operation from database")

		// Determine the new usage value.
		var newUsageValue float64
		currentUsageValue := subscription.GetCurrentUsageValue(*resourceType.ID)
		log.Debugf("the current usage value is %f", currentUsageValue)
		switch usage.UpdateType {
		case UpdateTypeSet:
			newUsageValue = usage.UsageValue
		case UpdateTypeAdd:
			newUsageValue = currentUsageValue + usage.UsageValue
		default:
			return fmt.Errorf("%w: %s", ErrInvalidUpdateType, usage.UpdateType)
		}
		log.Debugf("calculated the new usage to be %f", newUsageValue)

		// Update the usage.
		newUsage := &model.Usage{
			SubscriptionID: subscription.ID,
			ResourceTypeID: resourceType.ID,
			Usage:          newUsageValue,
		}
		err = qmsdb.UpsertUsage(ctx, tx, newUsage)
		if err != nil {
			return errors.Wrap(err, "unable to update or insert the usage record")
		}
		log.Debug("added/updated the usage record in the database")

		// Record the update in the database.
		update := model.Update{
			Value:             usage.UsageValue,
			ValueType:         model.ValueTypeUsages,
			EffectiveDate:     time.Now(),
			UpdateOperationID: updateOperation.ID,
			ResourceTypeID:    resourceType.ID,
			UserID:            subscription.UserID,
			Metadata:          &usage.Metadata,
		}
		err = qmsdb.SaveUpdate(ctx, tx, &update)
		if err != nil {
			return err
		}
		log.Debug("recorded the update in the databse")

		return nil
	})
}

// AddUsages adds or updates the usage record for a user, plan, and resource type.
func (s Server) AddUsages(ctx echo.Context) error {
	var (
		err   error
		usage Usage
	)

	log := log.WithFields(logrus.Fields{"context": "adding usage information"})

	context := ctx.Request().Context()

	// Extract and validate the request body.
	if err = ctx.Bind(&usage); err != nil {
		return model.Error(ctx, "invalid request body", http.StatusBadRequest)
	}

	log.Debugf("validated usage information %+v", usage)

	if err = s.addUsage(context, &usage); err != nil {
		log.Error(err)
		return model.Error(ctx, err.Error(), httpStatusCode(err))
	}

	log.Debugf("added usage inforamtion %+v", usage)
	username := strings.TrimSuffix(usage.Username, s.UsernameSuffix)
	successMsg := fmt.Sprintf("successfully updated the usage for: %s", username)

	return model.SuccessMessage(ctx, successMsg, http.StatusOK)
}

func (s Server) GetAllUsageOfUser(ctx echo.Context) error {
	var err error

	log := log.WithFields(logrus.Fields{"context": "getting all user usages"})

	context := ctx.Request().Context()

	username := strings.TrimSuffix(ctx.Param("username"), s.UsernameSuffix)
	if username == "" {
		return model.Error(ctx, "invalid username", http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{"user": username})

	// GetActiveSubscriptionDetails subscribes the user to the default plan when they have no active subscription, so
	// this read path writes and needs a transaction of its own.
	var subscription *model.Subscription
	err = s.goquTransaction(func(tx *goqu.TxDatabase) error {
		var err error
		subscription, err = qmsdb.GetActiveSubscriptionDetails(context, tx, username)
		return err
	})
	if err != nil {
		sCode := httpStatusCode(err)
		log.Error(err)
		return model.Error(ctx, err.Error(), sCode)
	}

	log.Info("successfully found usages")

	return model.Success(ctx, subscription.Usages, http.StatusOK)
}

func (s Server) GetAllUsageUpdatesForUser(ctx echo.Context) error {
	var err error

	log := log.WithFields(logrus.Fields{"context": "get all user updates"})

	username := strings.TrimSuffix(ctx.Param("username"), s.UsernameSuffix)
	if username == "" {
		return model.Error(ctx, "invalid username", http.StatusBadRequest)
	}
	log.WithFields(logrus.Fields{"user": username})

	err = s.ValidateUser(ctx, username)
	if err != nil {
		return nil
	}

	context := ctx.Request().Context()
	var updates []model.Update
	err = s.goquTransaction(func(tx *goqu.TxDatabase) error {
		var err error
		updates, err = qmsdb.ListUpdatesForUser(context, tx, username)
		return err
	})
	if err != nil {
		sCode := httpStatusCode(err)
		log.Error(err)
		return model.Error(ctx, err.Error(), sCode)
	}

	log.Info("successfully found updates")
	return model.Success(ctx, updates, http.StatusOK)
}
