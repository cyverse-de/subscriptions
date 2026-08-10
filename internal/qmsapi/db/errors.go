package db

import "errors"

var (
	ErrResourceTypeConflict = errors.New("a resource type with the same name already exists")

	// ErrNotFound reports a row that does not exist. The GORM layer signalled
	// this with gorm.ErrRecordNotFound. goqu does not raise an error at all:
	// ScanStruct/ScanVal report absence as a false "found" return with a nil
	// error, and sql.ErrNoRows never reaches the caller, so matching on it
	// would silently yield a zero-valued row instead of a not-found. Queries
	// translate that false return here at the boundary and callers keep
	// matching one sentinel. Every match on this is a 404-versus-500 decision.
	ErrNotFound = errors.New("record not found")
)
