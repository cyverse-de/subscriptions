package db

import (
	"github.com/cyverse-de/go-mod/logging"
	"github.com/doug-martin/goqu/v9"
	"github.com/sirupsen/logrus"

	"github.com/jmoiron/sqlx"
)

var log = logging.Log.WithFields(logrus.Fields{"package": "db"})

type Database struct {
	fullDB *goqu.Database
	goquDB GoquDatabase
	logSQL bool
}

func New(dbconn *sqlx.DB) *Database {
	goquDB := goqu.New("postgresql", dbconn)
	return &Database{
		fullDB: goquDB, // Used when a method needs to use a method not defined in the GoquDatabase interface.
		goquDB: goquDB, // Used when a method needs to optionally support being run inside a transaction.
		logSQL: false,  // Set to true to log SQL statements. TODO: implement for all statements.
	}
}

// EnableSQLLogging enables SQL logging for the database instance.
func (d *Database) EnableSQLLogging() {
	d.logSQL = true
}

// LogSQL logs an SQL statement that is being executed if debugging is enabled.
func (d *Database) LogSQL(statement SQLStatement) {
	if d.logSQL {
		sql, args, err := statement.ToSQL()
		if err != nil {
			log.Errorf("unable to generate the SQL: %s", err)
			return
		}
		log.Infof("%s %v", sql, args)
	}
}

func (d *Database) Begin() (*goqu.TxDatabase, error) {
	return d.fullDB.Begin()
}

func (d *Database) querySettings(opts ...QueryOption) (*QuerySettings, GoquDatabase) {
	var db GoquDatabase

	querySettings := &QuerySettings{}
	for _, opt := range opts {
		opt(querySettings)
	}

	if querySettings.tx != nil {
		db = querySettings.tx
	} else {
		db = d.goquDB
	}

	return querySettings, db
}

// querySettingsWithTX is the same as querySettings(), except it will return a
// new transaction if one is not passed in. Callers are responsible for managing
// rollbacks and commits.
func (d *Database) querySettingsWithTX(opts ...QueryOption) (*QuerySettings, *goqu.TxDatabase, error) {
	var db *goqu.TxDatabase

	querySettings := &QuerySettings{}
	for _, opt := range opts {
		opt(querySettings)
	}

	if querySettings.tx != nil {
		db = querySettings.tx
	} else {
		tx, err := d.Begin()
		if err != nil {
			return nil, nil, err
		}
		db = tx
		querySettings.tx = tx
		querySettings.doCommit = true
		querySettings.doRollback = true
	}

	return querySettings, db, nil
}

// QuerySettings provides configuration for queries, such as including a limit
// statement, an offset statement, or running the query as part of a transaction.
type QuerySettings struct {
	hasLimit   bool
	limit      uint
	hasOffset  bool
	offset     uint
	tx         *goqu.TxDatabase
	doRollback bool
	doCommit   bool
}

// QueryOption defines the signature for functions that can modify a QuerySettings
// instance.
type QueryOption func(*QuerySettings)

// WithQueryLimit allows callers to add a limit SQL statement to a query.
func WithQueryLimit(limit uint) QueryOption {
	return func(s *QuerySettings) {
		s.hasLimit = true
		s.limit = limit
	}
}

// WithQueryOffset allows callers to add an offset SQL statement to a query.
func WithQueryOffset(offset uint) QueryOption {
	return func(s *QuerySettings) {
		s.hasOffset = true
		s.offset = offset
	}
}

// WithTX allows callers to use a query as part of a transaction.
func WithTX(tx *goqu.TxDatabase) QueryOption {
	return func(s *QuerySettings) {
		s.tx = tx
	}
}

// WithTXRollbackCommit allows callers to control whether a function can call
// Rollback() and Commit() on the transaction, or if that should be left up to
// the caller to manage.
func WithTXRollbackCommit(tx *goqu.TxDatabase, doRollback, doCommit bool) QueryOption {
	return func(s *QuerySettings) {
		s.tx = tx
		s.doRollback = doRollback
		s.doCommit = doCommit
	}
}
