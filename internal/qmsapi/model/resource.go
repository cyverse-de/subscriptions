package model

import "fmt"

const (
	RESOURCE_TYPE_CPU_HOURS = "cpu.hours"
	RESOURCE_TYPE_DATA_SIZE = "data.size"
)

// ResourceTypeList represents a list of resource types.
//
// swagger:model
type ResourceTypeList struct {

	// The list of resource types
	ResourceTypes []*ResourceType `json:"resource_types"`
}

// GetResourceTypeByName returns the resource type from a resource type list with the given name.
func (rtl *ResourceTypeList) GetResourceTypeByName(name string) (*ResourceType, error) {
	for _, rt := range rtl.ResourceTypes {
		if rt.Name == name {
			return rt, nil
		}
	}
	return nil, fmt.Errorf("resoure type %s not found", name)
}

// ResourceType defines the structure for ResourceTypes.
//
// swagger:model
type ResourceType struct {
	// The resource type ID
	//
	// readOnly: true
	ID *string `gorm:"type:uuid;default:uuid_generate_v1()" json:"id,omitempty"`

	// The resource type name
	//
	// required: true
	Name string `gorm:"not null;unique" json:"name,omitempty"`

	// The unit of measure used for the resource type
	//
	// required: true
	Unit string `gorm:"not null;unique" json:"unit,omitempty"`

	// Indicates whether or not a resource is consumable. That is, whether or not using the resource permanently
	// consumes a portion of the allocation. For example, CPU hours are permanently consumed as soon as they're used,
	// so they would be considered consumable. Conversely, data storage can be reclaimed by removing files form the
	// data store, so data storage is not considered to be consumable.
	Consumable bool `json:"consumable"`
}
