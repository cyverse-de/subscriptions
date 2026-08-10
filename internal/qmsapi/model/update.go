package model

import (
	"time"
)

// Value type constants.
const (
	ValueTypeQuotas = "quotas"
	ValueTypeUsages = "usages"
)

// UpdateOperation defines the structure of an available update operation in the qms database.
//
// swagger:model
type UpdateOperation struct {
	// The update operation ID
	//
	// readOnly: true
	ID *string `db:"id" json:"id"`
	// The update operation name
	//
	// required: true
	Name string `db:"name" json:"name"`
}

type Update struct {
	ID                *string      `db:"id" json:"id"`
	ValueType         string       `db:"value_type" json:"value_type"`
	Value             float64      `db:"value" json:"value"`
	EffectiveDate     time.Time    `db:"effective_date" json:"effective_date"`
	UpdateOperationID *string      `db:"update_operation_id" json:"-"`
	ResourceTypeID    *string      `db:"resource_type_id" json:"-"`
	ResourceType      ResourceType `db:"-" json:"resource_types"`
	UserID            *string      `db:"user_id" json:"-"`
	User              User         `db:"-" json:"user"`
	Metadata          *string      `db:"metadata" json:"metadata"`
}
