package model

import "time"

// Usage define the structure for API Usages.
//
// swagger:model
type Usage struct {
	// The usage record identifier
	//
	// readOnly: true
	ID *string `db:"id" json:"id,omitempty"`

	// The usage amount
	Usage float64 `db:"usage" json:"usage"`

	// The subscription identifier
	SubscriptionID *string `db:"subscription_id" json:"-"`

	// The resource type identifier
	ResourceTypeID *string `db:"resource_type_id" json:"-"`

	// The resource type associated with the usage amount
	ResourceType ResourceType `db:"-" json:"resource_type,omitempty"`

	// The date and time the usage value was last modified
	LastModifiedAt *time.Time `db:"last_modified_at" json:"last_modified_at,omitempty"`
}
