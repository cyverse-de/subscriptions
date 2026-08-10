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
	ID *string `gorm:"type:uuid;default:uuid_generate_v1()" db:"id" json:"id"`
	// The update operation name
	//
	// required: true
	Name string `gorm:"type:text;not null;unique" db:"name" json:"name"`
}

type Update struct {
	ID                *string      `gorm:"type:uuid;default:uuid_generate_v1()" db:"id" json:"id"`
	ValueType         string       `db:"value_type" json:"value_type"`
	Value             float64      `gorm:"not null" db:"value" json:"value"`
	EffectiveDate     time.Time    `gorm:"type:date;not null" db:"effective_date" json:"effective_date"`
	UpdateOperationID *string      `gorm:"type:uuid;not null" db:"update_operation_id" json:"-"`
	ResourceTypeID    *string      `gorm:"type:uuid;not null" db:"resource_type_id" json:"-"`
	ResourceType      ResourceType `db:"-" json:"resource_types"`
	UserID            *string      `gorm:"type:uuid" db:"user_id" json:"-"`
	User              User         `db:"-" json:"user"`
	Metadata          *string      `db:"metadata" json:"metadata"`
}
