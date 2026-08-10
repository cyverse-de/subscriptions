package controllers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cyverse-de/echo-middleware/v2/params"
	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/httpmodel"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// extractPlanID extracts and validates the plan ID path parameter.
func extractPlanID(ctx echo.Context) (string, error) {
	planID, err := params.ValidatedPathParam(ctx, "plan_id", "uuid_rfc4122")
	if err != nil {
		return "", fmt.Errorf("the plan ID must be a valid UUID")
	}
	return planID, nil
}

// GetAllPlans is the handler for the GET /v1/plans endpoint.
//
// swagger:route GET /v1/plans plans listPlans
//
// # List Plans
//
// Lists all the plans that are currently available.
//
// responses:
//
//	200: plansResponse
//	400: badRequestResponse
//	500: internalServerErrorResponse
func (s Server) GetAllPlans(ctx echo.Context) error {
	log := log.WithFields(logrus.Fields{"context": "getting all plans"})

	context := ctx.Request().Context()

	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		plans, err := qmsdb.ListPlans(context, tx)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("listing plans from the database")

		return model.Success(ctx, plans, http.StatusOK)
	})
}

// GetPlanByID returns the plan with the given identifier.
//
// swagger:route GET /plans/{plan_id} plans getPlanByID
//
// # Get Plan Information
//
// Returns the plan with the given identifier.
//
// responses:
//
//	200: planResponse
//	400: badRequestResponse
//	500: internalServerErrorResponse
func (s Server) GetPlanByID(ctx echo.Context) error {
	var err error

	log := log.WithFields(logrus.Fields{"context": "getting plan by id"})

	context := ctx.Request().Context()

	// Extract and validate the plan ID.
	planID, err := extractPlanID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{"planID": planID})
	log.Debug("extracted and validated then plan ID from request")

	// Look up the plan.
	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		plan, err := qmsdb.GetPlanByID(context, tx, planID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("plan ID %s not found", planID)
			return txError(ctx, msg, http.StatusNotFound)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("successfully looked up plan to return")

		return model.Success(ctx, plan, http.StatusOK)
	})
}

// AddPlan adds a new plan to the database.
//
// swagger:route POST /plans plans addPlan
//
// # Add Plan
//
// Adds the plan to the Plans Database.
//
// Responses:
//
//	200: successMessageResponse
//	400: badRequestResponse
//	409: conflictResponse
//	500: internalServerErrorResponse
func (s Server) AddPlan(ctx echo.Context) error {
	var err error

	log := log.WithFields(logrus.Fields{"context": "adding plan"})

	context := ctx.Request().Context()

	// Parse and validate the request body.
	var plan httpmodel.NewPlan
	if err = ctx.Bind(&plan); err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	if err = plan.Validate(); err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{"plan": plan.Name})
	log.Debugf("adding a new plan to the database: %+v", plan)

	// Begin a transaction.
	return s.goquTransaction(context, func(tx *goqu.TxDatabase) error {
		dbPlan := plan.ToDBModel()

		// Make sure that a plan with the same name doesn't already exist.
		planNameExists, err := qmsdb.CheckPlanNameExistence(context, tx, plan.Name)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		} else if planNameExists {
			return txError(ctx, fmt.Sprintf("a plan named `%s` already exists", plan.Name), http.StatusBadRequest)
		}

		// Look up each resource type and update it in the struct.
		for i, planQuotaDefault := range dbPlan.PlanQuotaDefaults {
			resourceType, err := qmsdb.GetResourceTypeByName(context, tx, planQuotaDefault.ResourceType.Name)
			if errors.Is(err, qmsdb.ErrNotFound) {
				msg := fmt.Sprintf("resource type not found: %s", planQuotaDefault.ResourceType.Name)
				return txError(ctx, msg, http.StatusBadRequest)
			} else if err != nil {
				return txError(ctx, err.Error(), http.StatusInternalServerError)
			}
			dbPlan.PlanQuotaDefaults[i].ResourceType = *resourceType

			log.Debugf("adding plan quota default resource %s to plan %s", resourceType.Name, plan.Name)
		}
		log.Debugf("translated plan: %+v", dbPlan)

		// Add the plan to the database.
		err = qmsdb.SavePlan(context, tx, &dbPlan)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("successfully added plan to the database")

		return model.SuccessMessage(ctx, "Success", http.StatusOK)
	})
}

// GetActivePlanRate reports the active rate for an exisitng subscription plan.
//
// swagger:route GET /plans/{plan_id}/active-rate plans getPlanActiveRate
//
// # Get Active Rate for a Plan
//
// Returns the active rate for a plan. The active plan rate is the rate associated with the selected plan with the most
// recent effective date that is before the current date.
//
// Responses:
//
//	200: activePlanRateResponse
//	400: badRequestResponse
//	404: notFoundResponse
//	500: internalServerErrorResponse
func (s Server) GetActivePlanRate(ctx echo.Context) error {
	var err error

	// Extract and validate the plan ID.
	planID, err := extractPlanID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Initialize the logger and log a message indicating what is being done.
	log := log.WithFields(
		logrus.Fields{
			"context": "getting active plan rate",
			"plan_id": planID,
		},
	)
	log.Info("getting the active rate for a plan")

	// Begin a transaction.
	return s.goquTransaction(ctx.Request().Context(), func(tx *goqu.TxDatabase) error {
		context := ctx.Request().Context()

		// Verify that the plan exists.
		exists, err := qmsdb.CheckPlanExistence(context, tx, planID)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		} else if !exists {
			msg := fmt.Sprintf("plan ID %s not found", planID)
			return txError(ctx, msg, http.StatusNotFound)
		}

		// Look up the active plan rate.
		activePlanRate, err := qmsdb.GetActivePlanRate(context, tx, planID)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		return model.Success(ctx, activePlanRate, http.StatusOK)
	})
}

// GetActiveQuotaDefaults reports the active quota defaults for an existing subscription plan.
//
// swagger:route GET /plans/{plan_id}/active-quota-defaults plans getPlanActiveQuotaDefaults
//
// # Get Active Quota Defaults for a Plan
//
// Returns the active quota defaults for a plan. The active quota default value for a specific subscription plan for a
// specific resource type is always the quota default for that resource type with the most recent effective date not
// greater than the current date.
//
// Responses:
//
// 200: activePlanQuotaDefaultsResponse
// 400: badRequestResponse
// 404: notFoundResponse
// 500: internalServerErrorResponse
func (s Server) GetActiveQuotaDefaults(ctx echo.Context) error {
	var err error

	// Extract and validate the plan ID.
	planID, err := extractPlanID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Initialize the logger and log a message indicating that the plan is being updated.
	log := log.WithFields(
		logrus.Fields{
			"context": "getting active plan quota defaults",
			"plan_id": planID,
		},
	)
	log.Info("getting active plan quota defaults for an existing plan")

	// Begin a transaction.
	return s.goquTransaction(ctx.Request().Context(), func(tx *goqu.TxDatabase) error {
		context := ctx.Request().Context()

		// Verify that the plan exists.
		exists, err := qmsdb.CheckPlanExistence(context, tx, planID)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		} else if !exists {
			msg := fmt.Sprintf("plan ID %s not found", planID)
			return txError(ctx, msg, http.StatusNotFound)
		}

		// Look up the active plan quota defaults.
		activePlanQuotaDefaults, err := qmsdb.GetActivePlanQuotaDefaults(context, tx, planID)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		return model.Success(ctx, activePlanQuotaDefaults, http.StatusOK)
	})
}

// AddPlanQuotaDefaults adds quota defaults to an exisitng subscription plan.
//
// swagger:route POST /plans/{plan_id}/quota-defaults plans addPlanQuotaDefaults
//
// # Add Plan Quota Default Values
//
// Adds quota default values to an existing plan. The existing quota default values for the plan will be left in
// place. The effective quota default value for a specific subscription plan for a specific resource type is always the
// quota default for that resource type with the most recent effective date not greater than the current date.
//
// Responses:
//
//	200: planResponse
//	400: badRequestResponse
//	404: notFoundResponse
//	500: internalServerErrorResponse
func (s Server) AddPlanQuotaDefaults(ctx echo.Context) error {
	var err error

	// Extract and validate the plan ID.
	planID, err := extractPlanID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Initialize the logger and log a message indicating that the plan is being updated.
	log := log.WithFields(
		logrus.Fields{
			"context": "adding plan quota defaults",
			"plan_id": planID,
		},
	)
	log.Info("adding quota defaults to an existing plan")

	// Parse and validate the request body.
	var planQuotaDefaultList httpmodel.NewPlanQuotaDefaultList
	if err = ctx.Bind(&planQuotaDefaultList); err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	if err = planQuotaDefaultList.Validate(); err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Begin a transaction.
	return s.goquTransaction(ctx.Request().Context(), func(tx *goqu.TxDatabase) error {
		context := ctx.Request().Context()

		// Verify that the plan exists.
		plan, err := qmsdb.GetPlanByID(context, tx, planID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("plan ID %s not found", planID)
			return txError(ctx, msg, http.StatusNotFound)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		// Convert the request body to the equivalent database model and insert the plan ID into each object.
		planQuotaDefaults := planQuotaDefaultList.ToDBModel()
		for i := range planQuotaDefaults {
			planQuotaDefaults[i].PlanID = &planID
		}

		// Retireve the list of resource types from the database.
		resourceTypeList, err := qmsdb.ListResourceTypes(context, tx)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		// Plug the actual resource types into each plan quota default.
		for i, pqd := range planQuotaDefaults {
			rt, err := resourceTypeList.GetResourceTypeByName(pqd.ResourceType.Name)
			if err != nil {
				return txError(ctx, err.Error(), http.StatusBadRequest)
			}
			planQuotaDefaults[i].ResourceType = *rt
		}

		// Compare the plan quota defaults to existing quota defaults to check for duplicates.
		existingPlanQuotaDefaults := make(map[httpmodel.PlanQuotaDefaultKey]bool)
		for _, pqd := range plan.PlanQuotaDefaults {
			key := httpmodel.KeyFromPlanQuotaDefault(pqd)
			existingPlanQuotaDefaults[key] = true
		}
		for _, pqd := range planQuotaDefaults {
			key := httpmodel.KeyFromPlanQuotaDefault(pqd)
			if existingPlanQuotaDefaults[key] {
				msg := fmt.Sprintf(
					"plan quota default for resource type %s with effective date %s already exists",
					pqd.ResourceType.Name,
					pqd.EffectiveDate,
				)
				return txError(ctx, msg, http.StatusBadRequest)
			}
		}

		// Save the list of plan quota defaults.
		err = qmsdb.SavePlanQuotaDefaults(context, tx, planQuotaDefaults)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		// Look up the plan with the new plan quota defaults included and return it in the response.
		plan, err = qmsdb.GetPlanByID(context, tx, planID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("plan ID %s not found after saving it", planID)
			return txError(ctx, msg, http.StatusInternalServerError)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		return model.Success(ctx, plan, http.StatusOK)
	})
}

// AddRates adds rates to an existing subscription plan.
//
// swagger:route POST /plans/{plan_id}/rates plans addPlanRates
//
// # Add Plan Rates
//
// Adds rates to an existing plan. The existing rates for the plan will be left in place. The effective rate for a
// specific subscription plan is always the rate with the most recent effective date before the current date.
//
// Responses:
//
//	200: planResponse
//	400: badRequestResponse
//	404: notFoundResponse
//	500: internalServerErrorResponse
func (s Server) AddPlanRates(ctx echo.Context) error {
	var err error

	// Extract and validate the plan ID.
	planID, err := extractPlanID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Initialize the logger and log a message indicating that the plan is being updated.
	log := log.WithFields(
		logrus.Fields{
			"context": "adding plan rates",
			"plan_id": planID,
		},
	)
	log.Info("adding rates to an existing plan")

	// Parse and validate the request body.
	var planRateList httpmodel.NewPlanRateList
	if err = ctx.Bind(&planRateList); err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}
	if err = planRateList.Validate(); err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	// Begin a transaction.
	return s.goquTransaction(ctx.Request().Context(), func(tx *goqu.TxDatabase) error {
		context := ctx.Request().Context()

		// Verify that the plan existws.
		plan, err := qmsdb.GetPlanByID(context, tx, planID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("plan ID %s not found", planID)
			return txError(ctx, msg, http.StatusNotFound)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		// Convert the list of plan rates to the corresponding DB model.
		planRates := planRateList.ToDBModel()

		// Verify that none of the incoming plan rates duplicate existing plan rates.
		existingPlanRates := make(map[int64]bool)
		for _, pr := range plan.PlanRates {
			existingPlanRates[pr.EffectiveDate.UnixMilli()] = true
		}
		for _, pr := range planRates {
			if existingPlanRates[pr.EffectiveDate.UnixMilli()] {
				msg := fmt.Sprintf("plan rate with effective date %s already exists", pr.EffectiveDate)
				return txError(ctx, msg, http.StatusBadRequest)
			}
		}

		// Plug the plan ID into each of the plan rates.
		for i := range planRates {
			planRates[i].PlanID = &planID
		}

		// Save the list of plan rates.
		err = qmsdb.SavePlanRates(context, tx, planRates)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		// Look up the plan with the new plan quota defaults included and return it in the response.
		plan, err = qmsdb.GetPlanByID(context, tx, planID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("plan ID %s not found after saving it", planID)
			return txError(ctx, msg, http.StatusInternalServerError)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}
		return model.Success(ctx, plan, http.StatusOK)
	})
}
