package app

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/cyverse-de/go-mod/logging"
	"github.com/cyverse-de/go-mod/pbinit"
	"github.com/cyverse-de/p/go/ptypes"
	"github.com/cyverse-de/p/go/qms"
	"github.com/cyverse-de/subscriptions/common"
	"github.com/cyverse-de/subscriptions/db"
	"github.com/cyverse-de/subscriptions/errors"
	"github.com/cyverse-de/subscriptions/internal/qmsapi"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/controllers"
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

var log = logging.Log.WithFields(logrus.Fields{"package": "apps"})

// logDeprecatedRoute records a call to a route that has a replacement. It is the
// signal for when the old route can be deleted: once nothing logs this, nothing
// is calling it.
func logDeprecatedRoute(c echo.Context, replacement string) {
	log.WithFields(logrus.Fields{
		"method":      c.Request().Method,
		"uri":         c.Request().URL.Path,
		"replacement": replacement,
	}).Warn("deprecated route called")
}

// logRequest records the outcome of one request. The level follows the status so
// that a routine 404 doesn't read like an outage: only a 5xx is this service's
// fault, while a 4xx is worth seeing without being an error.
func logRequest(c echo.Context, v middleware.RequestLoggerValues) error {
	entry := log.WithFields(logrus.Fields{
		"method":  v.Method,
		"uri":     v.URI,
		"status":  v.Status,
		"latency": v.Latency.String(),
	})
	// Taken from the route rather than from a handler, so the name on the line is
	// always the subject of that request. The routes disagree on the parameter's
	// name, and requests that never matched a route have neither.
	if user := cmp.Or(c.Param("username"), c.Param("user")); user != "" {
		entry = entry.WithField("user", user)
	}
	if v.Error != nil {
		entry = entry.WithField("error", v.Error.Error())
	}

	switch {
	case v.Status >= http.StatusInternalServerError:
		entry.Error("request failed")
	case v.Status >= http.StatusBadRequest:
		entry.Warn("request rejected")
	default:
		entry.Info("request")
	}

	return nil
}

type App struct {
	db             *sqlx.DB
	Router         *echo.Echo
	userSuffix     string
	ReportOverages bool
}

func New(db *sqlx.DB, userSuffix string, reportOverages bool) *App {
	app := &App{
		db:             db,
		userSuffix:     userSuffix,
		Router:         echo.New(),
		ReportOverages: reportOverages,
	}

	// Recover from handler panics so a bug (e.g. a nil deref on a malformed
	// body) returns a 500 through the error handler instead of dropping the
	// connection.
	app.Router.Use(middleware.Recover())

	// Account for every request, whether or not its handler logs anything of its
	// own. Several routes -- the whole add-on tree among them -- log nothing at
	// all, so without this there is no way to tell from the logs that a request
	// even arrived.
	app.Router.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		// The greeting route is what the liveness and readiness probes call,
		// twice every five seconds. Logging it would bury real traffic.
		Skipper: func(c echo.Context) bool {
			return c.Request().Method == http.MethodGet && c.Request().URL.Path == "/"
		},
		LogMethod:  true,
		LogURI:     true,
		LogStatus:  true,
		LogLatency: true,
		LogError:   true,
		// Let the error handler run first, so the status logged here is the one
		// the client actually received.
		HandleError:   true,
		LogValuesFunc: logRequest,
	}))

	app.Router.HTTPErrorHandler = func(err error, c echo.Context) {
		// A handler that already wrote its response and then failed would
		// otherwise get a second JSON document appended to the first.
		if c.Response().Committed {
			return
		}

		var (
			code int
			body interface{}
		)

		switch err := err.(type) {
		case common.ErrorResponse:
			code = http.StatusBadRequest
			body = err
		case *common.ErrorResponse:
			code = http.StatusBadRequest
			body = err
		case *echo.HTTPError:
			echoErr := err
			code = echoErr.Code
			body = common.NewErrorResponse(err)
		default:
			// HTTPStatusCode resolves the sentinels and the ValidationError
			// wrapper, so a handler can report bad input by returning the error
			// rather than writing the response itself.
			code = errors.HTTPStatusCode(err)
			body = common.NewErrorResponse(err)
		}

		c.JSON(code, body) // nolint:errcheck
	}

	app.Router.GET("/", app.GreetingHTTPHandler).Name = "greeting"
	app.Router.GET("/summary/:user", app.GetUserSummaryHTTPHandler)
	app.Router.PUT("/addons", app.AddAddonHTTPHandler)
	app.Router.GET("/addons", app.ListAddonsHTTPHandler)
	app.Router.POST("/addons/:uuid", app.UpdateAddonHTTPHandler)
	app.Router.DELETE("/addons/:uuid", app.DeleteAddonHTTPHandler)
	app.Router.GET("/subscriptions/:uuid/addons", app.ListSubscriptionAddonsHTTPHandler)
	app.Router.PUT("/subscriptions/:sub_uuid/addons/:addon_uuid", app.AddSubscriptionAddonHTTPHandler)

	// A subscription add-on is addressed by its own UUID, which is what these
	// routes take. See the note above the handlers.
	app.Router.GET("/subscription-addons/:uuid", app.GetSubscriptionAddonByIDHTTPHandler)
	app.Router.POST("/subscription-addons/:uuid", app.UpdateSubscriptionAddonByIDHTTPHandler)
	app.Router.DELETE("/subscription-addons/:uuid", app.DeleteSubscriptionAddonByIDHTTPHandler)

	// Deprecated, and still what terrain calls. Their :addon_uuid segment is a
	// subscription add-on's UUID and their :sub_uuid segment is ignored.
	app.Router.GET("/subscriptions/:sub_uuid/addons/:addon_uuid", app.GetSubscriptionAddonHTTPHandler)
	app.Router.DELETE("/subscriptions/:sub_uuid/addons/:addon_uuid", app.DeleteSubscriptionAddonHTTPHandler)
	app.Router.POST("/subscriptions/:sub_uuid/addons/:addon_uuid", app.UpdateSubscriptionAddonHTTPHandler)
	app.Router.PUT("/users", app.AddUserHTTPHandler)
	app.Router.GET("/users/:username/updates", app.GetUserUpdatesHTTPHandler)
	app.Router.PUT("/user/:username/updates", app.AddUserUpdateHTTPHandler)
	app.Router.GET("/users/:username/overages", app.GetUserOveragesHTTPHandler)
	app.Router.GET("/users/:username/overages/:resource_name", app.CheckUserOveragesHTTPHandler)
	app.Router.GET("/users/:username/usages", app.GetUsagesHTTPHandler)
	app.Router.PUT("/users/:username/usages", app.AddUsageHTTPHandler)
	app.Router.GET("/plans", app.ListPlansHTTPHandler)
	app.Router.PUT("/plans", app.AddPlanHTTPHandler)
	app.Router.GET("/plans/:plan_id", app.GetPlanHTTPHandler)
	app.Router.PUT("/quotas", app.AddQuotaHTTPHandler)

	return app
}

// RegisterQMSAPI mounts the QMS /v1 API on this app's router, sharing the
// database connection the rest of the service already uses.
//
// It is separate from New because New is also called without a database by
// tests that exercise only the paths which return before touching one.
func (a *App) RegisterQMSAPI(usernameSuffix string) {
	a.Router.Validator = qmsapi.NewCustomValidator()
	qmsapi.RegisterHandlers(controllers.Server{
		Router:         a.Router,
		DB:             a.db.DB,
		GoquDB:         db.New(a.db),
		Service:        "subscriptions",
		Title:          "CyVerse Subscriptions",
		UsernameSuffix: usernameSuffix,
	})
}

func (a *App) FixUsername(username string) (string, error) {

	re, err := regexp.Compile(`@.*$`)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(username, ""), nil
}

func (a *App) validateUpdate(request *qms.AddUpdateRequest) (string, error) {
	// The lax JSON decoder accepts requests missing nested objects, so guard
	// before dereferencing; the old protojson codec never produced these.
	if request.GetUpdate() == nil || request.GetUpdate().GetUser() == nil ||
		request.GetUpdate().GetResourceType() == nil || request.GetUpdate().GetOperation() == nil {
		return "", errors.ErrInvalidRequestBody
	}

	username, err := a.FixUsername(request.Update.User.Username)
	if err != nil {
		return "", err
	}

	if request.Update.ResourceType.Name == "" || !lo.Contains[string](
		db.ResourceTypeNames,
		request.Update.ResourceType.Name,
	) {
		return username, errors.ErrInvalidResourceName
	}

	if request.Update.ResourceType.Unit == "" || !lo.Contains[string](
		db.ResourceTypeUnits,
		request.Update.ResourceType.Unit,
	) {
		return username, errors.ErrInvalidResourceUnit
	}

	if request.Update.Operation.Name == "" || !lo.Contains[string](
		db.UpdateOperationNames,
		request.Update.Operation.Name,
	) {
		return username, errors.ErrInvalidOperationName
	}

	if request.Update.ValueType == "" || !lo.Contains[string](
		[]string{db.UsagesTrackedMetric, db.QuotasTrackedMetric},
		request.Update.ValueType,
	) {
		return username, errors.ErrInvalidValueType
	}

	if request.Update.EffectiveDate == nil {
		return username, errors.ErrInvalidEffectiveDate
	}

	return username, nil
}

func (a *App) GreetingHTTPHandler(ctx echo.Context) error {
	return ctx.String(http.StatusOK, "Hello from subscriptions.")
}

func (a *App) getUserUpdates(ctx context.Context, request *qms.UpdateListRequest) *qms.UpdateListResponse {
	response := pbinit.NewQMSUpdateListResponse()

	if request.GetUser() == nil {
		response.Error = errors.NatsError(ctx, errors.ErrInvalidRequestBody)
		return response
	}

	username, err := a.FixUsername(request.User.Username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	d := db.New(a.db)

	mUpdates, err := d.UserUpdates(ctx, username)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	response.Updates = make([]*qms.Update, 0, len(mUpdates))
	for _, mu := range mUpdates {
		response.Updates = append(response.Updates, &qms.Update{
			Uuid:          mu.ID,
			EffectiveDate: ptypes.New(mu.EffectiveDate),
			ValueType:     mu.ValueType,
			Value:         mu.Value,
			ResourceType: &qms.ResourceType{
				Uuid:       mu.ResourceType.ID,
				Name:       mu.ResourceType.Name,
				Unit:       mu.ResourceType.Unit,
				Consumable: mu.ResourceType.Consumable,
			},
			Operation: &qms.UpdateOperation{
				Uuid: mu.UpdateOperation.ID,
				Name: mu.UpdateOperation.Name,
			},
			User: &qms.QMSUser{
				Uuid:     mu.User.ID,
				Username: mu.User.Username,
			},
		})
	}

	return response
}

func (a *App) GetUserUpdatesHTTPHandler(c echo.Context) error {
	ctx := c.Request().Context()

	request := &qms.UpdateListRequest{
		User: &qms.QMSUser{
			Username: c.Param("username"),
		},
	}

	response := a.getUserUpdates(ctx, request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}

func (a *App) addUserUpdate(ctx context.Context, request *qms.AddUpdateRequest) *qms.AddUpdateResponse {
	var (
		err                                 error
		userID, resourceTypeID, operationID string
		update                              *db.Update
	)

	response := pbinit.NewQMSAddUpdateResponse()

	// Validate the request.
	username, err := a.validateUpdate(request)
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}

	// Add the username to the logger as a field for debugging. The shadow is
	// deliberate: assigning to the package logger would leave this username on
	// every later request's log lines, whoever they were about.
	log := log.WithFields(logrus.Fields{"user": username})

	// Create a new database client.
	d := db.New(a.db)

	// Begin a transaction.
	tx, err := d.Begin()
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
		return response
	}
	err = tx.Wrap(func() error {
		// Get the userID if it's not provided
		if request.Update.User.Uuid == "" {
			log.Infof("getting user ID for %s", username)
			user, err := d.EnsureUser(ctx, username, db.WithTX(tx))
			if err != nil {
				return err
			}
			userID = user.ID
			log.Infof("user ID for %s is %s", username, userID)
		} else {
			userID = request.Update.User.Uuid
			log.Infof("user ID from request is %s", userID)
		}

		// Get the resource type id if it's not provided.
		if request.Update.ResourceType.Uuid == "" {
			log.Infof("getting resource type id for resource '%s'", request.Update.ResourceType.Name)
			resourceTypeID, err = d.GetResourceTypeID(
				ctx,
				request.Update.ResourceType.Name,
				request.Update.ResourceType.Unit,
				db.WithTX(tx),
			)
			if err != nil {
				return err
			}
			log.Infof("resource type id for resource %s is '%s'", request.Update.ResourceType.Name, resourceTypeID)
		} else {
			resourceTypeID = request.Update.ResourceType.Uuid
			log.Infof("resource type id from request is %s", resourceTypeID)
		}

		// Get the operation id if it's not provided.
		if request.Update.Operation.Uuid == "" {
			log.Infof("getting operation ID for %s", request.Update.Operation.Name)
			operationID, err = d.GetOperationID(
				ctx,
				request.Update.Operation.Name,
				db.WithTX(tx),
			)
			if err != nil {
				return err
			}
			log.Infof("operation ID for %s is %s", request.Update.Operation.Name, operationID)
		} else {
			operationID = request.Update.Operation.Uuid
			log.Infof("using operation ID %s from request", request.Update.Operation.Uuid)
		}

		// Construct the model.Update
		update = &db.Update{
			ValueType:     request.Update.ValueType,
			Value:         request.Update.Value,
			EffectiveDate: request.Update.EffectiveDate.AsTime(),
			ResourceType: db.ResourceType{
				ID:         resourceTypeID,
				Name:       request.Update.ResourceType.Name,
				Unit:       request.Update.ResourceType.Unit,
				Consumable: request.Update.ResourceType.Consumable,
			},
			User: db.User{
				ID:       userID,
				Username: username,
			},
			UpdateOperation: db.UpdateOperation{
				ID:   operationID,
				Name: request.Update.Operation.Name,
			},
			Metadata: request.Update.Metadata,
		}

		// Add the update to the database. AddUserUpdate will populate the structure with the update ID.
		log.Info("adding update to the database")
		_, err = d.AddUserUpdate(ctx, update, db.WithTX(tx))
		if err != nil {
			return err
		}
		log.Info("done adding update to the database")

		// Process the update.
		switch update.ValueType {
		case db.UsagesTrackedMetric:
			log.Info("processing update for usage")
			if err = d.ProcessUpdateForUsage(ctx, update, db.WithTX(tx)); err != nil {
				return err
			}
			log.Info("after processing update for usage")

		case db.QuotasTrackedMetric:
			log.Info("processing update for quota")
			if err = d.ProcessUpdateForQuota(ctx, update, db.WithTX(tx)); err != nil {
				return err
			}
			log.Info("after processing update for quota")

		default:
			return fmt.Errorf("unknown value type in update: %s", update.ValueType)
		}

		// Look up the recorded update and store it in the response.
		recordedUpdate, err := d.GetUserUpdate(ctx, update.ID, db.WithTX(tx))
		if err != nil {
			return err
		}
		if recordedUpdate == nil {
			return fmt.Errorf("unable to find the user update after recording it: %s", update.ID)
		}
		response.Update = &qms.Update{
			Uuid:      recordedUpdate.ID,
			ValueType: recordedUpdate.ValueType,
			Value:     recordedUpdate.Value,
			ResourceType: &qms.ResourceType{
				Uuid:       recordedUpdate.ResourceType.ID,
				Name:       recordedUpdate.ResourceType.Name,
				Unit:       recordedUpdate.ResourceType.Unit,
				Consumable: recordedUpdate.ResourceType.Consumable,
			},
			EffectiveDate: ptypes.New(recordedUpdate.EffectiveDate),
			Operation: &qms.UpdateOperation{
				Uuid: recordedUpdate.UpdateOperation.ID,
				Name: recordedUpdate.UpdateOperation.Name,
			},
			User: &qms.QMSUser{
				Uuid:     update.User.ID,
				Username: update.User.Username,
			},
		}

		return nil
	})
	if err != nil {
		response.Error = errors.NatsError(ctx, err)
	}
	return response
}

func (a *App) AddUserUpdateHTTPHandler(c echo.Context) error {
	var (
		err     error
		request qms.AddUpdateRequest
	)

	ctx := c.Request().Context()

	if err = c.Bind(&request); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": "bad request",
		})
	}

	// The path username is authoritative, but a body missing the nested objects would panic on the
	// assignment below before validateUpdate ever sees it.
	if request.GetUpdate() == nil || request.GetUpdate().GetUser() == nil {
		return c.JSON(http.StatusBadRequest, map[string]string{
			"message": errors.ErrInvalidRequestBody.Error(),
		})
	}
	request.Update.User.Username = c.Param("username")

	response := a.addUserUpdate(ctx, &request)

	if response.Error != nil {
		return c.JSON(int(response.Error.StatusCode), response)
	}

	return c.JSON(http.StatusOK, response)
}
