package model

import (
	"time"

	"github.com/cyverse-de/subscriptions/internal/qmsapi/model/timestamp"
)

// SubscriptionOptions represents options that can be applied to a new subscription.
//
// swagger: model
type SubscriptionOptions struct {
	// True if the user paid for the subscription.
	Paid *bool `json:"paid"`

	// The number of periods included in the subscription.
	Periods *int32 `json:"periods"`

	// The effective start date of the subscription.
	StartDate *timestamp.Timestamp `json:"start_date"`

	// The effective end date of the subscription.
	EndDate *timestamp.Timestamp `json:"end_date"`
}

// Return the appropriate paid flag for the subscription options.
func (o *SubscriptionOptions) IsPaid() bool {
	if o.Paid == nil {
		return false
	} else {
		return *o.Paid
	}
}

// Return the number of periods for the subscription options.
func (o *SubscriptionOptions) GetPeriods() int32 {
	if o.Periods == nil {
		return 1
	} else {
		return *o.Periods
	}
}

// Return the effective start date for the subscription options.
func (o *SubscriptionOptions) GetStartDate() time.Time {
	if o.StartDate == nil {
		return time.Now()
	} else {
		return time.Time(*o.StartDate)
	}
}

// Return the effective end date for the subscription options.
func (o *SubscriptionOptions) GetEndDate(startDate time.Time) time.Time {
	if o.EndDate == nil {
		return startDate.AddDate(int(o.GetPeriods()), 0, 0)
	} else {
		return time.Time(*o.EndDate)
	}
}

// SubscriptionRequest represents a request for a single subscription.
//
// swagger: model
type SubscriptionRequest struct {
	SubscriptionOptions

	// The username to associate with the subscription
	//
	// required: true
	Username *string `json:"username"`

	// The name of the plan associated with the subscription
	//
	// required: true
	PlanName *string `json:"plan_name"`
}

// SubscriptionRequests represents a list of subscription requests.
//
// swagger: model
type SubscriptionRequests struct {
	// The list of subscriptions to create
	//
	// required: true
	Subscriptions []SubscriptionRequest `json:"subscriptions"`
}

// SubscriptionResponse represents a response to a request for a single subscription.
//
// swagger: model
type SubscriptionResponse struct {
	Subscription

	// The reason the subscription couldn't be created if an error occurred.
	FailureReason *string `json:"failure_reason,omitempty"`

	// True if the subscription was just created.
	NewSubscription bool `json:"new_subscription"`
}

// SubscriptionResponseFromSubscription converts a user plan to a subscription response.
func SubscriptionResponseFromSubscription(subscription *Subscription, newSubscription bool) *SubscriptionResponse {
	var resp SubscriptionResponse
	resp.Subscription = *subscription
	resp.NewSubscription = newSubscription
	return &resp
}

// SubscriptionListing represents a list of subscriptions.
//
// swagger: model
type SubscriptionListing struct {
	// The subscriptions in the listing.
	Subscriptions []*Subscription `json:"subscriptions"`

	// The total number of matched subscriptions.
	Total int64 `json:"total"`
}
