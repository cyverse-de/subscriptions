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

func (a *App) ListPlansHandler(subject, reply string, request *qms.NoParamsRequest) {
	var err error
	log := log.WithField("context", "list plans")

	ctx, span := pbinit.InitQMSNoParamsRequest(request, subject)
	defer span.End()

	response := a.listPlans(ctx)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
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

func (a *App) AddPlanHandler(subject, reply string, request *qms.AddPlanRequest) {
	var err error
	log := log.WithField("context", "list plans")

	ctx, span := pbinit.InitQMSAddPlanRequest(request, subject)
	defer span.End()

	response := a.addPlan(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
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

	response.Plan = &qms.Plan{
		Uuid:              plan.ID,
		Name:              plan.Name,
		Description:       plan.Description,
		PlanQuotaDefaults: []*qms.QuotaDefault{},
	}

	for _, q := range plan.QuotaDefaults {
		response.Plan.PlanQuotaDefaults = append(
			response.Plan.PlanQuotaDefaults,
			&qms.QuotaDefault{
				Uuid:       q.ID,
				QuotaValue: q.QuotaValue,
				ResourceType: &qms.ResourceType{
					Uuid:       q.ResourceType.ID,
					Name:       q.ResourceType.Name,
					Unit:       q.ResourceType.Unit,
					Consumable: q.ResourceType.Consumable,
				},
			})
	}

	return response
}

func (a *App) GetPlanHandler(subject, reply string, request *qms.PlanRequest) {
	var err error
	log := log.WithField("context", "get plan")

	ctx, span := pbinit.InitQMSPlanRequest(request, subject)
	defer span.End()

	response := a.getPlan(ctx, request)

	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) GetPlanHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &qms.PlanRequest{
		PlanId: c.Param("plan_id"),
	}

	response := a.getPlan(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) upsertQuotaDefault(ctx context.Context, _ *qms.AddPlanQuotaDefaultRequest) *qms.QuotaDefaultResponse {
	response := pbinit.NewQuotaDefaultResponse()
	response.Error = errors.NatsError(ctx, fmt.Errorf("not implemented"))
	return response
}

func (a *App) UpsertQuotaDefaultsHandler(subject, reply string, request *qms.AddPlanQuotaDefaultRequest) {
	var err error

	ctx, span := pbinit.InitQMSAddPlanQuotaDefaultRequest(request, subject)
	defer span.End()

	response := a.upsertQuotaDefault(ctx, request)
	if response.Error != nil {
		log.Error(response.Error.Message)
	}

	if err = a.client.Respond(ctx, reply, response); err != nil {
		log.Error(err)
	}
}

func (a *App) UpsertQuotaDefaultsHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddPlanQuotaDefaultRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	response := a.upsertQuotaDefault(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
