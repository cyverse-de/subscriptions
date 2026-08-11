package app

import (
	"fmt"

	serrors "github.com/cyverse-de/subscriptions/errors"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// requireUUID reports a malformed UUID as bad input.
//
// Without this the value reaches a query verbatim and PostgreSQL rejects it:
// "invalid input syntax for type uuid". That surfaces as a 500 -- the caller's
// typo reported as this service's fault -- carrying the driver's own message
// back to the client.
func requireUUID(value, name string) error {
	if _, err := uuid.Parse(value); err != nil {
		return serrors.AsBadRequest(fmt.Errorf("%s must be a UUID", name))
	}
	return nil
}

// uuidParam returns a UUID path parameter, reporting a malformed one as bad
// input.
func uuidParam(c echo.Context, name string) (string, error) {
	value := c.Param(name)
	if err := requireUUID(value, name); err != nil {
		return "", err
	}
	return value, nil
}
