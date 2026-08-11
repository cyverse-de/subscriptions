package controllers

import (
	"errors"
	"fmt"
	"net/http"

	qmsdb "github.com/cyverse-de/subscriptions/internal/qmsapi/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
	"github.com/labstack/echo/v4"
)

// dbError reports a failed database operation as a server error. A driver error names tables, indexes, constraints and
// PostgreSQL error codes, so the error itself is logged and the caller is told only which operation failed. Pass
// attempted as a description of the operation, such as "unable to list the plans".
func dbError(ctx echo.Context, err error, attempted string) error {
	log.WithError(err).Error(attempted)
	return model.Error(ctx, attempted, http.StatusInternalServerError)
}

// txDBError is dbError for use inside a transaction callback, where the response has to be wrapped so that the
// transaction rolls back rather than committing the write it is reporting as failed.
func txDBError(ctx echo.Context, err error, attempted string) error {
	log.WithError(err).Error(attempted)
	return txError(ctx, attempted, http.StatusInternalServerError)
}

// ValidateUser determines whether or not a username exists in the database. If an error occurs during the lookup or
// the user doesn't exist then the appropriate response will be sent to the caller and an error will be returned.
func (s Server) ValidateUser(ctx echo.Context, username string) error {
	var exists bool
	err := s.goquTransaction(ctx.Request().Context(), func(tx *goqu.TxDatabase) error {
		var err error
		exists, err = qmsdb.UserExists(ctx.Request().Context(), tx, username)
		return err
	})
	if err != nil {
		sendErr := dbError(ctx, err, "unable to look up the user")
		if sendErr != nil {
			ctx.Logger().Errorf("unable to send response: %s", sendErr.Error())
		}
		return err
	}
	if !exists {
		msg := fmt.Sprintf("user %s does not exist", username)
		sendErr := model.Error(ctx, msg, http.StatusNotFound)
		if sendErr != nil {
			ctx.Logger().Errorf("unable to send response: %s", sendErr.Error())
		}
		return errors.New(msg)
	}
	return nil
}
