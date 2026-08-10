package controllers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/cyverse-de/echo-middleware/v2/params"
	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

// extractResourceTypeID extracts and validates the resource type ID path parameter.
func extractResourceTypeID(ctx echo.Context) (string, error) {
	resourceTypeID, err := params.ValidatedPathParam(ctx, "resource_type_id", "uuid_rfc4122")
	if err != nil {
		return "", fmt.Errorf("the resource type ID must be a valid UUID")
	}
	return resourceTypeID, nil
}

// swagger:route GET /v1/resource-types resource-types listResourceTypes
//
// List Resource Types
//
// Lists all the resource types defined in the qms database.
//
// responses:
//   200: resourceTypeListing
//   500: internalServerErrorResponse

// ListResourceTypes is the handler for the GET /v1/resource-types and GET /v1/resource-types endpoints.
func (s Server) ListResourceTypes(ctx echo.Context) error {
	context := ctx.Request().Context()

	log := log.WithFields(logrus.Fields{"context": "listing resource types"})

	return s.goquTransaction(func(tx *goqu.TxDatabase) error {
		resourceTypes, err := qmsdb.ListResourceTypes(context, tx)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("found resource types to return")

		return model.Success(ctx, resourceTypes.ResourceTypes, http.StatusOK)
	})
}

// swagger:route POST /v1/resource-types resource-types addResourceType
//
// Add Resource Type
//
// Adds a new resource type to the qms database.
//
// responses:
//   200: resourceTypeDetails
//   400: badRequestResponse
//   409: conflictResponse
//   500: internalServerErrorResponse

// AddResourceType is the handler for the POST /v1/resource-types endpoint.
func (s Server) AddResourceType(ctx echo.Context) error {
	context := ctx.Request().Context()
	var err error

	log := log.WithFields(logrus.Fields{"context": "adding resource type"})

	//  Extract and validate the request body.
	var resourceType model.ResourceType
	if err = ctx.Bind(&resourceType); err != nil {
		msg := fmt.Sprintf("invalid request body: %s", err)
		return model.Error(ctx, msg, http.StatusBadRequest)
	}
	if resourceType.Name == "" || resourceType.Unit == "" {
		msg := "the resource type name and unit are both required"
		return model.Error(ctx, msg, http.StatusBadRequest)
	}

	log.Debugf("adding resource type %s with unit %s", resourceType.Name, resourceType.Unit)

	// Guard against the case where the ID is specified in the request body, which breaks our duplicate check.
	resourceType.ID = nil

	// Save the resource type.
	return s.goquTransaction(func(tx *goqu.TxDatabase) error {
		populatedResourceType, err := qmsdb.SaveResourceType(context, tx, resourceType)
		if errors.Is(err, qmsdb.ErrResourceTypeConflict) {
			return txError(ctx, err.Error(), http.StatusConflict)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		return model.Success(ctx, populatedResourceType, http.StatusOK)
	})
}

// swagger:route GET /v1/resource-types/{resource_type_id} resource-types getResourceTypeDetails
//
// Get Resource Type Details
//
// Returns information about the resource type with the given identifier.
//
// responses:
//   200: resourceTypeDetails
//   400: badRequestResponse
//   404: notFoundResponse
//   500: internalServerErrorResponse

// GetResourceTypeDetails is the handler for the GET /v1/resource-types/{resource_type_id} endpoint.
func (s Server) GetResourceTypeDetails(ctx echo.Context) error {
	context := ctx.Request().Context()
	var err error

	log := log.WithFields(logrus.Fields{"context": "getting resource type details"})

	// Extract and validate the resource type ID.
	resourceTypeID, err := extractResourceTypeID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{"resourceTypeID": resourceTypeID})
	log.Debugf("extracted resource type ID %s", resourceTypeID)

	// Look up the resource type.
	return s.goquTransaction(func(tx *goqu.TxDatabase) error {
		resourceType, err := qmsdb.GetResourceTypeByID(context, tx, resourceTypeID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("resource type not found: %s", resourceTypeID)
			return txError(ctx, msg, http.StatusNotFound)
		} else if err != nil {
			msg := fmt.Sprintf("unable to look up the resource type: %s", err)
			return txError(ctx, msg, http.StatusInternalServerError)
		}

		log.Debug("found resource type information to return")

		return model.Success(ctx, resourceType, http.StatusOK)
	})
}

// swagger:route PUT /v1/resource-types/{resource_type_id} resource-types updateResourceType
//
// Update Resource Type
//
// Updates an existing resource type in the qms database.
//
// responses:
//   200: resourceTypeDetails
//   400: badRequestResponse
//   404: notFoundResponse
//   409: conflictResponse
//   500: internalServerErrorResponse

// UpdateResourceType is the handler for the PUT /v1/resource-types/{resource_type_id} endpoint.
func (s Server) UpdateResourceType(ctx echo.Context) error {
	log := log.WithFields(logrus.Fields{"context": "updating resource type"})
	context := ctx.Request().Context()
	var err error

	// Extract and validate the resource type ID.
	resourceTypeID, err := extractResourceTypeID(ctx)
	if err != nil {
		return model.Error(ctx, err.Error(), http.StatusBadRequest)
	}

	log = log.WithFields(logrus.Fields{"resourceTypeID": resourceTypeID})

	//  Extract and validate the request body.
	var inboundResourceType model.ResourceType
	if err = ctx.Bind(&inboundResourceType); err != nil {
		msg := fmt.Sprintf("invalid request body: %s", err)
		return model.Error(ctx, msg, http.StatusBadRequest)
	}
	if inboundResourceType.Name == "" || inboundResourceType.Unit == "" {
		msg := "the resource type name and unit are both required"
		return model.Error(ctx, msg, http.StatusBadRequest)
	}

	log.Debug("extracted and validated the request body")

	// Perform these steps in a transaction to ensure consistency.
	return s.goquTransaction(func(tx *goqu.TxDatabase) error {
		var err error

		// Verify that the resource type exists.
		existingResourceType, err := qmsdb.GetResourceTypeByID(context, tx, resourceTypeID)
		if errors.Is(err, qmsdb.ErrNotFound) {
			msg := fmt.Sprintf("resource type not found: %s", resourceTypeID)
			return txError(ctx, msg, http.StatusNotFound)
		} else if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		log.Debug("verified that the resource type exists")

		// Verify that a different resource type with the new name doesn't exist already. A failed lookup is reported as
		// a conflict rather than a server error; that is carried over from the GORM implementation deliberately.
		homonym, err := qmsdb.GetResourceTypeByName(context, tx, inboundResourceType.Name)
		if err != nil && !errors.Is(err, qmsdb.ErrNotFound) {
			return txError(ctx, err.Error(), http.StatusConflict)
		} else if err == nil && *homonym.ID != *existingResourceType.ID {
			msg := fmt.Sprintf("a resource type with the given name already exists: %s", inboundResourceType.Name)
			return txError(ctx, msg, http.StatusConflict)
		}

		// Update the resource type.
		existingResourceType.Name = inboundResourceType.Name
		existingResourceType.Unit = inboundResourceType.Unit
		existingResourceType.Consumable = inboundResourceType.Consumable
		err = qmsdb.UpdateResourceType(context, tx, *existingResourceType)
		if err != nil {
			return txError(ctx, err.Error(), http.StatusInternalServerError)
		}

		return model.Success(ctx, existingResourceType, http.StatusOK)
	})
}
