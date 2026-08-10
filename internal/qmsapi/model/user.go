package model

// User User
//
// swagger:model
type User struct {

	// The user ID
	//
	// in: path
	// required: true
	ID *string `db:"id" json:"id,omitempty"`

	// The username
	//
	// in: path
	// required: true
	Username string `db:"username" json:"username,omitempty"`
}
