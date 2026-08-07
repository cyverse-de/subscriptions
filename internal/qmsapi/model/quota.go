package model

import "time"

// Quota represents a resource usage limit associated with a subscription.
//
// swagger:model
type Quota struct {
	// The quota identifier
	//
	// readOnly: true
	ID *string `gorm:"type:uuid;default:uuid_generate_v1()" json:"id,omitempty"`

	// The resource usage limit
	Quota float64 `json:"quota"`

	// The user plan ID
	SubscriptionID *string `gorm:"type:uuid;not null" json:"-"`

	// The resource type ID
	ResourceTypeID *string `gorm:"type:uuid;not null" json:"-"`

	// The resource type associated with this quota
	ResourceType ResourceType `json:"resource_type,omitempty"`

	// The date and time the quota was last modified
	LastModifiedAt *time.Time `gorm:"->" json:"last_modified_at,omitempty"`
}

// TableName specifies the table name to use the database.
func (q *Quota) TableName() string {
	return "quotas"
}
