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
		sendErr := model.Error(ctx, err.Error(), http.StatusInternalServerError)
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
