package db

import "errors"

var (
	ErrResourceTypeConflict = errors.New("a resource type with the same name already exists")
)
