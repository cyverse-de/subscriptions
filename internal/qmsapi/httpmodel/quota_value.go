package httpmodel

// QuotaValue represents a single quota value for which the resource type name is provided elsewhere.
//
// swagger: model
type QuotaValue struct {
	// The resource usage limit.
	Quota float64 `json:"quota" validate:"required"`
}
