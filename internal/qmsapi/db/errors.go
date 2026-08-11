package db

import "errors"

var (
	ErrResourceTypeConflict = errors.New("a resource type with the same name already exists")

	// ErrNotFound reports a row that does not exist. The GORM layer signalled
	// this with its own ErrRecordNotFound. goqu does not raise an error at all:
	// ScanStruct/ScanVal report absence as a false "found" return with a nil
	// error, and sql.ErrNoRows never reaches the caller, so matching on it
	// would silently yield a zero-valued row instead of a not-found. Queries
	// translate that false return here at the boundary and callers keep
	// matching one sentinel. Every match on this is a 404-versus-500 decision.
	ErrNotFound = errors.New("record not found")

	// ErrUnsupportedUpdateType reports an update operation this layer has no
	// arithmetic for. Handlers reject an operation that isn't in
	// update_operations before reaching a write, so this only fires for one
	// that exists in the table but has never been implemented here.
	ErrUnsupportedUpdateType = errors.New("unsupported update type")

	// ErrEmptySlice reports a bulk write that was handed nothing to write. It
	// reproduces GORM's ErrEmptySlice, which Create raised whenever its
	// destination was a zero-length slice, verbatim: the two plan write
	// endpoints accept a body whose list is empty and answer 500 with this
	// message. goqu raises nothing at all for the same input — it emits
	// INSERT ... DEFAULT VALUES, which would write a garbage row — so every
	// converted bulk write has to check the length itself.
	ErrEmptySlice = errors.New("empty slice found")
)
