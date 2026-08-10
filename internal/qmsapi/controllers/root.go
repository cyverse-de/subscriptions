package controllers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"maps"
	"net/http"

	"github.com/cyverse-de/go-mod/logging"
	"github.com/cyverse-de/subscriptions/db"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
	"github.com/labstack/echo/v4"
	"github.com/sirupsen/logrus"
)

var log = logging.Log.WithFields(logrus.Fields{"package": "controllers"})

// txAbort carries an error response that has already been written. model.Error returns nil once the response is
// written, so returning it directly from a transaction callback tells the transaction machinery to commit the very
// write the response is reporting as failed; wrapping it in an error that machinery can see is what rolls the
// transaction back.
type txAbort struct {
	response error
}

func (a txAbort) Error() string {
	return "transaction rolled back after an error response was written"
}

// txError writes an error response and rolls the enclosing transaction back. Use it in place of model.Error inside a
// transaction callback.
func txError(ctx echo.Context, errStr string, status int) error {
	return txAbort{response: model.Error(ctx, errStr, status)}
}

// Server defines the REST API of the qms
type Server struct {
	Router         *echo.Echo
	DB             *sql.DB
	GoquDB         *db.Database
	Service        string
	Title          string
	Version        string
	ReportOverages bool
	UsernameSuffix string
}

// runCallback runs a transaction callback, rolling the transaction back and re-panicking if it panics so that
// middleware.Recover still turns the panic into a 500.
func runCallback(tx *goqu.TxDatabase, fn func(tx *goqu.TxDatabase) error) error {
	defer func() {
		if p := recover(); p != nil {
			rollback(tx)
			panic(p)
		}
	}()
	return fn(tx)
}

// rollback rolls a failed transaction back. A failed ROLLBACK is logged rather than reported, because the error that
// caused the rollback is the one the caller has to answer with.
func rollback(tx *goqu.TxDatabase) {
	if err := tx.Rollback(); err != nil {
		log.Errorf(
			"unable to roll the transaction back: %s; the transaction had either already finished or lost its "+
				"database connection", err,
		)
	}
}

// goquTransaction runs fn inside a database transaction, unwrapping the response written by any txError call within it
// so that echo sees the handler's real return value. The transaction is started with the request context so that a
// caller that has already given up doesn't leave a goroutine waiting for a free connection.
//
// fn must not write the response: the caller owns it and reports the returned error itself, which is what lets
// POST /v1/subscriptions record a per-item failure rather than failing the whole request. Handlers whose callback
// produces the response use respondInTransaction instead.
//
// The transaction is committed and rolled back here rather than through goqu's (*TxDatabase).Wrap because Wrap
// replaces the callback's error with the ROLLBACK error whenever ROLLBACK itself fails. That loses the txAbort marker
// this unwraps, and reports transaction plumbing to the caller in place of the failure the handler actually saw.
func (s Server) goquTransaction(ctx context.Context, fn func(tx *goqu.TxDatabase) error) error {
	tx, err := s.GoquDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err := runCallback(tx, fn); err != nil {
		rollback(tx)

		var abort txAbort
		if errors.As(err, &abort) {
			return abort.response
		}

		return err
	}

	return tx.Commit()
}

// respondInTransaction runs fn inside a database transaction, buffering the response fn writes and sending it only
// once the transaction has committed. Writing before COMMIT reports a write as successful over the wire and then
// discards it if COMMIT fails, leaving the caller believing in a record that doesn't exist and with nothing to
// prompt a retry.
//
// Because nothing reaches the client while the buffer is in place, every failure is answered with model.Error, so a
// database outage is reported in the /v1 envelope that this API's consumers decode rather than in echo's.
func (s Server) respondInTransaction(ctx echo.Context, fn func(tx *goqu.TxDatabase) error) error {
	tx, err := s.GoquDB.BeginTx(ctx.Request().Context(), nil)
	if err != nil {
		log.Errorf("unable to start a transaction: %s", err)
		return model.Error(ctx, err.Error(), http.StatusInternalServerError)
	}

	// The deferred restore covers the panic path, where the response has to be back in place for
	// middleware.Recover's 500 to reach the client. Everything else restores it explicitly, before writing.
	buffered := &bufferedResponse{header: make(http.Header)}
	response := ctx.Response()
	ctx.SetResponse(echo.NewResponse(buffered, s.Router))
	defer ctx.SetResponse(response)

	cbErr := runCallback(tx, fn)
	ctx.SetResponse(response)

	if cbErr != nil {
		rollback(tx)

		// txError asked for the rollback that just happened, so the response it buffered is the handler's real
		// answer and describes a write that correctly never landed.
		var abort txAbort
		if errors.As(cbErr, &abort) {
			if err := buffered.flush(response); err != nil {
				return err
			}
			return abort.response
		}

		log.Errorf("the transaction was rolled back: %s", cbErr)
		return model.Error(ctx, cbErr.Error(), http.StatusInternalServerError)
	}

	if err := tx.Commit(); err != nil {
		// The buffered response reports a write that COMMIT has just discarded, so it is dropped rather than sent.
		log.Errorf("unable to commit the transaction: %s", err)
		return model.Error(ctx, err.Error(), http.StatusInternalServerError)
	}

	return buffered.flush(response)
}

// bufferedResponse collects a response in memory so that it can be sent, or discarded, once the transaction that
// produced it has been resolved.
type bufferedResponse struct {
	header http.Header
	body   bytes.Buffer
	status int
}

func (b *bufferedResponse) Header() http.Header {
	return b.header
}

func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func (b *bufferedResponse) Write(body []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(body)
}

// flush sends the buffered response. A buffer with no status was never written to, which leaves the response to echo
// exactly as an unbuffered handler that wrote nothing would have.
func (b *bufferedResponse) flush(response *echo.Response) error {
	if b.status == 0 {
		return nil
	}

	maps.Copy(response.Header(), b.header)
	response.WriteHeader(b.status)
	_, err := response.Write(b.body.Bytes())
	return err
}

// ServerInfo returns basic information about the server.
func (s Server) ServerInfo() *model.RootResponse {
	return &model.RootResponse{
		Service: s.Service,
		Title:   s.Title,
		Version: s.Version,
	}
}

// RootHandler handles GET requests to the / endpoint.
//
// swagger:route GET / misc getRoot
//
// # General API Information
//
// Lists general information about the service API itself.
//
// responses:
//
//	200: rootResponse
func (s Server) RootHandler(ctx echo.Context) error {
	return model.Success(ctx, s.ServerInfo(), http.StatusOK)
}

// V1RootHandler handles GET requests to the /v1 endpoint.
//
// swagger:route GET /v1 misc getV1Root
//
// # General API Version 1 Information
//
// Lists general information about version 1 of the service API.
//
// responses:
//
//	200: apiVersionResponse
func (s Server) V1RootHandler(ctx echo.Context) error {
	resp := model.APIVersionResponse{
		RootResponse: *s.ServerInfo(),
		APIVersion:   "1.0.0",
	}
	return model.Success(ctx, resp, http.StatusOK)
}
