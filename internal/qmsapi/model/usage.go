package model

import "time"

// Usage define the structure for API Usages.
//
// swagger:model
type Usage struct {
	// The usage record identifier
	//
	// readOnly: true
	ID *string `gorm:"type:uuid;default:uuid_generate_v1()" json:"id,omitempty"`

	// The usage amount
	Usage float64 `gorm:"not null" json:"usage"`

	// The subscription identifier
	SubscriptionID *string `gorm:"type:uuid;not null;index:usage_subscription_resourcetype,unique" json:"-"`

	// The resource type identifier
	ResourceTypeID *string `gorm:"type:uuid;not null;index:usage_subscription_resourcetype,unique" json:"-"`

	// The resource type associated with the usage amount
	ResourceType ResourceType `json:"resource_type,omitempty"`

	// The date and time the usage value was last modified
	LastModifiedAt *time.Time `gorm:"->" json:"last_modified_at,omitempty"`
}
