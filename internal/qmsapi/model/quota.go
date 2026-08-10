package model

import "time"

// Quota represents a resource usage limit associated with a subscription.
//
// swagger:model
type Quota struct {
	// The quota identifier
	//
	// readOnly: true
	ID *string `db:"id" json:"id,omitempty"`

	// The resource usage limit
	Quota float64 `db:"quota" json:"quota"`

	// The user plan ID
	SubscriptionID *string `db:"subscription_id" json:"-"`

	// The resource type ID
	ResourceTypeID *string `db:"resource_type_id" json:"-"`

	// The resource type associated with this quota
	ResourceType ResourceType `db:"-" json:"resource_type,omitempty"`

	// The date and time the quota was last modified
	LastModifiedAt *time.Time `db:"last_modified_at" json:"last_modified_at,omitempty"`
}
