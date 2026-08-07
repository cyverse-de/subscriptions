// Package qmsapi serves the QMS /v1 API from within the subscriptions service.
// It is a lift-and-shift of cyverse/qms: the routes, handlers, models and
// queries are carried over unchanged so that terrain, which resolves this API
// through terrain.qms.base-uri and parses its {result, error, status} envelope,
// sees no difference. Converging it with the service's own routes and query
// layer is deliberately separate work.
package qmsapi

import (
	"github.com/cyverse-de/subscriptions/internal/qmsapi/controllers"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
)

// CustomValidator represents a validator that Echo can use to check incoming requests.
type CustomValidator struct {
	validator *validator.Validate
}

// NewCustomValidator returns the validator the /v1 handlers expect. Echo has no
// validator by default, and AddUser calls ctx.Validate, so registering this is
// not optional.
func NewCustomValidator() *CustomValidator {
	return &CustomValidator{validator: validator.New()}
}

// Validate performs validation for an incoming request.
func (cv CustomValidator) Validate(i interface{}) error {
	return cv.validator.Struct(i)
}

func registerUserEndpoints(users *echo.Group, s *controllers.Server) {
	// Lists all of the users.
	users.GET("", s.GetAllUsers)

	// Adds a new user to the database.
	users.PUT("/:username", s.AddUser)

	// Gets a user's current plan details
	users.GET("/:username/plan", s.GetSubscriptionDetails)

	// Updates a quota in the user's current subscription plan.
	users.POST("/:username/plan/:resource-type/quota", s.UpdateCurrentSubscriptionQuota)

	// Changes the user's current plan to one corresponding to plan name.
	users.PUT("/:username/:plan_name", s.UpdateSubscription)

	users.GET("/:username/subscriptions", s.ListUserSubscriptions)
}

func registerPlanEndpoints(plans *echo.Group, s *controllers.Server) {
	// Returns a listing of all available plans
	plans.GET("", s.GetAllPlans)

	// Adds a plan to the database.
	plans.POST("", s.AddPlan)

	// Gets the details of a plan by its UUID.
	plans.GET("/:plan_id", s.GetPlanByID)

	// Reports the active rate for a plan.
	plans.GET("/:plan_id/active-rate", s.GetActivePlanRate)

	// Reports the active quota defaults for a plan.
	plans.GET("/:plan_id/active-quota-defaults", s.GetActiveQuotaDefaults)

	// Adds quota defaults to an existing plan.
	plans.POST("/:plan_id/quota-defaults", s.AddPlanQuotaDefaults)

	// Adds rates to an existing plan.
	plans.POST("/:plan_id/rates", s.AddPlanRates)
}

func registerResourceTypeEndpoints(resourceTypes *echo.Group, s *controllers.Server) {
	// Lists the available resource types.
	resourceTypes.GET("", s.ListResourceTypes)

	// Adds a new resource type to the database
	resourceTypes.POST("", s.AddResourceType)

	// Get the details about a resource type.
	resourceTypes.GET("/:resource_type_id", s.GetResourceTypeDetails)

	// Update details for a resource type.
	resourceTypes.PUT("/:resource_type_id", s.UpdateResourceType)
}

// RegisterHandlers mounts the QMS /v1 API on the router in s.
//
// QMS also served its service information from GET /, but that path already
// belongs to the subscriptions greeting, so it isn't registered here; the same
// information is still available from GET /v1. Nothing calls the root endpoint
// except health checks, which only look at the status code.
func RegisterHandlers(s controllers.Server) {
	v1 := s.Router.Group("/v1")
	v1.GET("", s.V1RootHandler)

	plans := v1.Group("/plans")
	registerPlanEndpoints(plans, &s)

	subscriptions := v1.Group("/subscriptions")
	subscriptions.POST("", s.AddSubscriptions)
	subscriptions.POST("/", s.AddSubscriptions)
	subscriptions.GET("", s.ListSubscriptions)
	subscriptions.GET("/", s.ListSubscriptions)

	usages := v1.Group("/usages")
	usages.GET("/:username", s.GetAllUsageOfUser)
	usages.POST("", s.AddUsages)
	usages.GET("/:username/updates", s.GetAllUsageUpdatesForUser)

	users := v1.Group("/users")
	registerUserEndpoints(users, &s)

	resourceTypes := v1.Group("/resource-types")
	registerResourceTypeEndpoints(resourceTypes, &s)
}
