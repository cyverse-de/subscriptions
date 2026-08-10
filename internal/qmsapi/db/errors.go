package db

import "errors"

var (
	ErrResourceTypeConflict = errors.New("a resource type with the same name already exists")

	// ErrNotFound reports a row that does not exist. The GORM layer signalled
	// this with gorm.ErrRecordNotFound; goqu over sqlx reports sql.ErrNoRows,
	// so queries translate at the boundary and callers keep matching on one
	// sentinel. Every match on this is a 404-versus-500 decision.
	ErrNotFound = errors.New("record not found")
)
