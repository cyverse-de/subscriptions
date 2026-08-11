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
)

func (a *App) listPlans(ctx context.Context) *qms.PlanList {
	response := pbinit.NewPlanList()

	d := db.New(a.db)
	plans, err := d.ListPlans(ctx)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	response.Plans = make([]*qms.Plan, len(plans))
	for i, p := range plans {
		response.Plans[i] = p.ToQMSPlan()
	}

	return response
}

func (a *App) ListPlansHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	response := a.listPlans(ctx)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) addPlan(ctx context.Context, request *qms.AddPlanRequest) *qms.PlanResponse {
	response := pbinit.NewPlanResponse()

	d := db.New(a.db)

	tx, err := d.Begin()
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	err = tx.Wrap(func() error {
		incomingPlan := db.NewPlanFromQMS(request.Plan)
		err := incomingPlan.Validate()
		if err != nil {
			return err
		}

		existingPlan, err := d.GetPlanByName(ctx, incomingPlan.Name, db.WithTX(tx))
		if err != nil {
			return err
		} else if existingPlan != nil {
			return fmt.Errorf("a plan named %s already exists", incomingPlan.Name)
		}

		for i, pqd := range incomingPlan.QuotaDefaults {
			rt, err := d.LookupResoureType(ctx, &pqd.ResourceType, db.WithTX(tx))
			if err != nil {
				return err
			}
			incomingPlan.QuotaDefaults[i].ResourceType = *rt
		}

		err = incomingPlan.ValidateQuotaDefaultUniqueness()
		if err != nil {
			return err
		}

		err = incomingPlan.ValidatePlanRateUniqueness()
		if err != nil {
			return err
		}

		newPlanID, err := d.AddPlan(ctx, incomingPlan, db.WithTX(tx))
		if err != nil {
			return err
		}

		plan, err := d.GetPlanByID(ctx, newPlanID, db.WithTX(tx))
		if err != nil {
			return err
		}

		response.Plan = plan.ToQMSPlan()
		return nil
	})

	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	return response
}

func (a *App) AddPlanHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddPlanRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	response := a.addPlan(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) getPlan(ctx context.Context, request *qms.PlanRequest) *qms.PlanResponse {
	response := pbinit.NewPlanResponse()

	d := db.New(a.db)

	plan, err := d.GetPlanByID(ctx, request.PlanId)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	response.Plan = plan.ToQMSPlan()

	return response
}

func (a *App) GetPlanHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	planID, err := uuidParam(c, "plan_id")
	if err != nil {
		return err
	}

	request := &qms.PlanRequest{
		PlanId: planID,
	}

	response := a.getPlan(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
