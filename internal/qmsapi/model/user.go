package model

// User User
//
// swagger:model
type User struct {

	// The user ID
	//
	// in: path
	// required: true
	ID *string `gorm:"type:uuid;default:uuid_generate_v1()" json:"id,omitempty"`

	// The username
	//
	// in: path
	// required: true
	Username string `gorm:"not null;unique" json:"username,omitempty"`
}
