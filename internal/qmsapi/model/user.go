package model

// User User
//
// swagger:model
type User struct {

	// The user ID
	//
	// in: path
	// required: true
	ID *string `gorm:"type:uuid;default:uuid_generate_v1()" db:"id" json:"id,omitempty"`

	// The username
	//
	// in: path
	// required: true
	Username string `gorm:"not null;unique" db:"username" json:"username,omitempty"`
}
