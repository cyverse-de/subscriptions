package db

import (
	"context"
	"fmt"
	"time"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/doug-martin/goqu/v9"
)

// SubscriptionOptions contains options for a new subscription.
type SubscriptionOptions struct {
	Paid    bool
	Periods int32
	EndDate time.Time
}

// DefaultSubscriptionOptions returns the default subscription options.
func DefaultSubscriptionOptions() *SubscriptionOptions {
	return &SubscriptionOptions{
		Paid:    false,
		Periods: 1,
		EndDate: time.Now().AddDate(1, 0, 0),
	}
}

// subscriptionDS returns the goqu.SelectDataset for getting user plan info, but with
// out the goqu.Where() calls.
func subscriptionDS(db GoquDatabase) *goqu.SelectDataset {
	return db.From(t.Subscriptions).
		Select(
			t.Subscriptions.Col("id").As("id"),
			t.Subscriptions.Col("effective_start_date").As("effective_start_date"),
			t.Subscriptions.Col("effective_end_date").As("effective_end_date"),
			t.Subscriptions.Col("created_by").As("created_by"),
			t.Subscriptions.Col("created_at").As("created_at"),
			t.Subscriptions.Col("last_modified_by").As("last_modified_by"),
			t.Subscriptions.Col("last_modified_at").As("last_modified_at"),
			t.Subscriptions.Col("paid").As("paid"),

			t.Users.Col("id").As(goqu.C("users.id")),
			t.Users.Col("username").As(goqu.C("users.username")),

			t.Plans.Col("id").As(goqu.C("plans.id")),
			t.Plans.Col("name").As(goqu.C("plans.name")),
			t.Plans.Col("description").As(goqu.C("plans.description")),

			t.PlanRates.Col("id").As(goqu.C("plan_rates.id")),
			t.PlanRates.Col("effective_date").As(goqu.C("plan_rates.effective_date")),
			t.PlanRates.Col("rate").As(goqu.C("plan_rates.rate")),
		).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Join(t.Plans, goqu.On(t.Subscriptions.Col("plan_id").Eq(t.Plans.Col("id")))).
		Join(t.PlanRates, goqu.On(t.Subscriptions.Col("plan_rate_id").Eq(t.PlanRates.Col("id"))))
}

func (d *Database) GetSubscriptionByID(ctx context.Context, subscriptionID string, opts ...QueryOption) (*Subscription, error) {
	_, db := d.querySettings(opts...)

	ds := subscriptionDS(db).
		Where(
			t.Subscriptions.Col("id").Eq(subscriptionID),
		)
	d.LogSQL(ds)

	var result Subscription
	found, err := ds.Executor().ScanStructContext(ctx, &result)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	return &result, nil
}

// GetActiveSubscription returns the active user plan for the username passed
// in, or nil when the user has no active subscription. Accepts a variable
// number of QueryOptions, but only WithTX is currently supported.
//
// Callers that need the user subscribed rather than merely looked up should use
// GetOrCreateActiveSubscription.
func (d *Database) GetActiveSubscription(ctx context.Context, username string, opts ...QueryOption) (*Subscription, error) {
	var (
		err    error
		found  bool
		result Subscription
		db     GoquDatabase
	)

	_, db = d.querySettings(opts...)

	effStartDate := goqu.I("subscriptions.effective_start_date")
	effEndDate := goqu.I("subscriptions.effective_end_date")
	currTS := goqu.L("CURRENT_TIMESTAMP")

	query := subscriptionDS(db).
		Where(
			t.Users.Col("username").Eq(username),
			goqu.Or(
				currTS.Between(goqu.Range(effStartDate, effEndDate)),
				goqu.And(currTS.Gt(effStartDate), effEndDate.Is(nil)),
			),
		).
		Order(effStartDate.Desc()).
		Limit(1)
	d.LogSQL(query)

	// The found flag is the only signal that the user has no active
	// subscription: an unfound scan leaves result zeroed, and a zeroed ID is an
	// empty string, which every query downstream compares against a uuid column.
	if found, err = query.Executor().ScanStructContext(ctx, &result); err != nil {
		return nil, err
	}
	if !found {
		log.Debugf("no active subscription for %s", username)
		return nil, nil
	}

	log.Debugf("%+v", result)

	return &result, nil
}

// GetOrCreateActiveSubscription returns the user's active subscription,
// subscribing them to the default plan when they have none. The user is created
// too if this is the first the service has heard of them.
func (d *Database) GetOrCreateActiveSubscription(
	ctx context.Context, username string, opts ...QueryOption,
) (*Subscription, error) {
	subscription, err := d.GetActiveSubscription(ctx, username, opts...)
	if err != nil {
		log.Errorf("unable to get the active subscription: %s", err)
		return nil, err
	}
	if subscription != nil {
		return subscription, nil
	}

	user, err := d.EnsureUser(ctx, username, opts...)
	if err != nil {
		log.Errorf("unable to ensure that %s exists in the database: %s", username, err)
		return nil, err
	}
	// EnsureUser upserts and reads back in one statement, so a transaction that
	// inserts the same new username first leaves the insert with nothing to
	// return and the read behind the statement's snapshot. The caller retrying
	// will find the row committed.
	if user == nil {
		err = fmt.Errorf("%s was created by a concurrent request; retry the request", username)
		log.Error(err)
		return nil, err
	}

	plan, err := d.GetPlanByName(ctx, DefaultPlanName, opts...)
	if err != nil {
		log.Errorf("unable to look up the %s plan to subscribe %s to: %s", DefaultPlanName, username, err)
		return nil, err
	}
	if plan == nil {
		err = fmt.Errorf("the default subscription plan %s does not exist", DefaultPlanName)
		log.Error(err)
		return nil, err
	}

	subscriptionID, err := d.SetActiveSubscription(ctx, user.ID, plan, DefaultSubscriptionOptions(), opts...)
	if err != nil {
		log.Errorf("unable to subscribe %s to the %s plan: %s", username, DefaultPlanName, err)
		return nil, err
	}

	subscription, err = d.GetSubscriptionByID(ctx, subscriptionID, opts...)
	if err != nil {
		log.Errorf("unable to look up the new subscription for %s: %s", username, err)
		return nil, err
	}
	if subscription == nil {
		err = fmt.Errorf("the subscription just created for %s could not be found", username)
		log.Error(err)
		return nil, err
	}

	return subscription, nil
}

func (d *Database) SetActiveSubscription(
	ctx context.Context, userID string, plan *Plan, subscriptionOpts *SubscriptionOptions, opts ...QueryOption,
) (string, error) {
	_, db := d.querySettings(opts...)

	n := time.Now()
	e := subscriptionOpts.EndDate

	// Get the active plan rate.
	activePlanRate := plan.GetActiveRate()
	if activePlanRate == nil {
		return "", fmt.Errorf("the %s subscription plan has no effective rate", plan.Name)
	}

	query := db.Insert(t.Subscriptions).
		Rows(
			goqu.Record{
				"effective_start_date": n,
				"effective_end_date":   e,
				"user_id":              userID,
				"plan_id":              plan.ID,
				"created_by":           "de",
				"last_modified_by":     "de",
				"paid":                 subscriptionOpts.Paid,
				"plan_rate_id":         activePlanRate.ID,
			},
		).
		Returning(t.Subscriptions.Col("id"))
	d.LogSQL(query)

	var subscriptionID string
	if _, err := query.Executor().ScanValContext(ctx, &subscriptionID); err != nil {
		return "", err
	}

	// Add the quota defaults as the t.Quotas for the user plan.
	for _, quotaDefault := range plan.GetActiveQuotaDefaults() {
		quotaValue := quotaDefault.QuotaValue * float64(subscriptionOpts.Periods)
		if quotaDefault.ResourceType.Consumable {
			quotaValue *= float64(subscriptionOpts.Periods)
		}
		ds := db.Insert(t.Quotas).
			Cols(
				"resource_type_id",
				"subscription_id",
				"quota",
				"created_by",
				"last_modified_by",
			).Rows(
			goqu.Record{
				"resource_type_id": quotaDefault.ResourceType.ID,
				"subscription_id":  subscriptionID,
				"quota":            quotaValue,
				"created_by":       goqu.V("de"),
				"last_modified_by": goqu.V("de"),
			},
		)
		d.LogSQL(ds)
		if _, err := ds.Executor().Exec(); err != nil {
			return subscriptionID, err
		}
	}

	return subscriptionID, nil
}

func (d *Database) UserHasActivePlan(ctx context.Context, username string, opts ...QueryOption) (bool, error) {
	var (
		err error
		db  GoquDatabase
	)

	_, db = d.querySettings(opts...)

	effStartDate := goqu.I("subscriptions.effective_start_date")
	effEndDate := goqu.I("subscriptions.effective_end_date")

	statement := db.From(t.Subscriptions).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Where(
			t.Users.Col("username").Eq(username),
			goqu.Or(
				CurrentTimestamp.Between(goqu.Range(effStartDate, effEndDate)),
				goqu.And(CurrentTimestamp.Gt(effStartDate), effEndDate.Is(nil)),
			),
		)
	d.LogSQL(statement)

	numPlans, err := statement.CountContext(ctx)
	if err != nil {
		return false, err
	}

	return numPlans > 0, nil
}

func (d *Database) UserOnPlan(ctx context.Context, username, planName string, opts ...QueryOption) (bool, error) {
	var err error

	_, db := d.querySettings(opts...)

	effStartDate := goqu.I("subscriptions.effective_start_date")
	effEndDate := goqu.I("subscriptions.effective_end_date")

	statement := db.From(t.Subscriptions).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Join(t.Plans, goqu.On(t.Subscriptions.Col("plan_id").Eq(t.Plans.Col("id")))).
		Where(
			t.Users.Col("username").Eq(username),
			t.Plans.Col("name").Eq(planName),
			goqu.Or(
				CurrentTimestamp.Between(goqu.Range(effStartDate, effEndDate)),
				goqu.And(CurrentTimestamp.Gt(effStartDate), effEndDate.Is(nil)),
			),
		)
	d.LogSQL(statement)

	numPlans, err := statement.CountContext(ctx)
	if err != nil {
		return false, err
	}

	return numPlans > 0, nil
}

// SubscriptionUsages returns a list of Usages associated with a user plan specified
// by the passed in UUID. Accepts a variable number of QueryOptions, though only
// WithTX is currently supported.
func (d *Database) SubscriptionUsages(ctx context.Context, subscriptionID string, opts ...QueryOption) ([]Usage, error) {
	var (
		err    error
		db     GoquDatabase
		usages []Usage
	)

	_, db = d.querySettings(opts...)

	usagesQuery := db.From(t.Usages).
		Select(
			t.Usages.Col("id").As("id"),
			t.Usages.Col("usage").As("usage"),
			t.Usages.Col("subscription_id").As("subscription_id"),
			t.Usages.Col("created_by").As("created_by"),
			t.Usages.Col("created_at").As("created_at"),
			t.Usages.Col("last_modified_by").As("last_modified_by"),
			t.Usages.Col("last_modified_at").As("last_modified_at"),
			t.RT.Col("id").As(goqu.C("resource_types.id")),
			t.RT.Col("name").As(goqu.C("resource_types.name")),
			t.RT.Col("unit").As(goqu.C("resource_types.unit")),
			t.RT.Col("consumable").As(goqu.C("resource_types.consumable")),
		).
		Join(t.RT, goqu.On(goqu.I("usages.resource_type_id").Eq(goqu.I("resource_types.id")))).
		Where(t.Usages.Col("subscription_id").Eq(subscriptionID))
	d.LogSQL(usagesQuery)

	if err = usagesQuery.Executor().ScanStructsContext(ctx, &usages); err != nil {
		return nil, err
	}

	return usages, nil
}

// SubscriptionPlanRates returns a list of rates assocaited with a user plan specified by the passed in UUID. Accepts a
// variable number of QueryOptions, though only WithTX is currently supported.
func (d *Database) SubscriptionPlanRates(ctx context.Context, planID string, opts ...QueryOption) ([]PlanRate, error) {
	var (
		err   error
		db    GoquDatabase
		rates []PlanRate
	)

	_, db = d.querySettings(opts...)

	ratesQuery := db.From(t.PlanRates).
		Select(
			t.PlanRates.Col("id").As("id"),
			t.PlanRates.Col("plan_id").As("plan_id"),
			t.PlanRates.Col("effective_date").As("effective_date"),
			t.PlanRates.Col("rate").As("rate"),
		).
		Where(t.PlanRates.Col("plan_id").Eq(planID))
	d.LogSQL(ratesQuery)

	if err = ratesQuery.Executor().ScanStructsContext(ctx, &rates); err != nil {
		return nil, err
	}
	return rates, nil
}

// SubscriptionQuotas returns a list of t.Quotas associated with the user plan specified
// by the UUID passed in. Accepts a variable number of QueryOptions, though only
// WithTX is currently supported.
func (d *Database) SubscriptionQuotas(ctx context.Context, subscriptionID string, opts ...QueryOption) ([]Quota, error) {
	var (
		err    error
		db     GoquDatabase
		quotas []Quota
	)

	_, db = d.querySettings(opts...)

	quotasQuery := db.From(t.Quotas).
		Select(
			t.Quotas.Col("id").As("id"),
			t.Quotas.Col("quota").As("quota"),
			t.Quotas.Col("subscription_id").As("subscription_id"),
			t.Quotas.Col("created_by").As("created_by"),
			t.Quotas.Col("created_at").As("created_at"),
			t.Quotas.Col("last_modified_by").As("last_modified_by"),
			t.Quotas.Col("last_modified_at").As("last_modified_at"),
			t.RT.Col("id").As(goqu.C("resource_types.id")),
			t.RT.Col("name").As(goqu.C("resource_types.name")),
			t.RT.Col("unit").As(goqu.C("resource_types.unit")),
			t.RT.Col("consumable").As(goqu.C("resource_types.consumable")),
		).
		Join(t.RT, goqu.On(goqu.I("quotas.resource_type_id").Eq(goqu.I("resource_types.id")))).
		Where(t.Quotas.Col("subscription_id").Eq(subscriptionID))
	d.LogSQL(quotasQuery)

	if err = quotasQuery.Executor().ScanStructsContext(ctx, &quotas); err != nil {
		return nil, err
	}

	return quotas, nil
}

// SubscriptionQuotaDefaults returns a list of PlanQuotaDefaults associated with the
// plan (not user plan, just plan) specified by the UUID passed in. Accepts a
// variable number of QueryOptions, though only WithTX is currently supported.
func (d *Database) SubscriptionQuotaDefaults(ctx context.Context, planID string, opts ...QueryOption) ([]PlanQuotaDefault, error) {
	var (
		err      error
		db       GoquDatabase
		defaults []PlanQuotaDefault
	)

	_, db = d.querySettings(opts...)

	pqdQuery := db.From(t.PQD).
		Select(
			t.PQD.Col("id").As("id"),
			t.PQD.Col("quota_value").As("quota_value"),
			t.PQD.Col("plan_id").As("plan_id"),
			t.PQD.Col("effective_date").As("effective_date"),
			t.RT.Col("id").As(goqu.C("resource_types.id")),
			t.RT.Col("name").As(goqu.C("resource_types.name")),
			t.RT.Col("unit").As(goqu.C("resource_types.unit")),
			t.RT.Col("consumable").As(goqu.C("resource_types.consumable")),
		).
		Join(t.RT, goqu.On(goqu.I("plan_quota_defaults.resource_type_id").Eq(goqu.I("resource_types.id")))).
		Where(t.PQD.Col("plan_id").Eq(planID))
	d.LogSQL(pqdQuery)

	if err = pqdQuery.Executor().ScanStructsContext(ctx, &defaults); err != nil {
		return nil, err
	}

	return defaults, nil
}

// LoadSubscriptionDetails adds PlanQuotaDefaults, quotas and usages into a user plan. Accepts a variable number of
// QuotaOptions, though only WithTX is currently supported.
func (d *Database) LoadSubscriptionDetails(ctx context.Context, subscription *Subscription, opts ...QueryOption) error {
	var (
		err      error
		defaults []PlanQuotaDefault
		usages   []Usage
		quotas   []Quota
	)

	defaults, err = d.SubscriptionQuotaDefaults(ctx, subscription.Plan.ID, opts...)
	if err != nil {
		return err
	}

	quotas, err = d.SubscriptionQuotas(ctx, subscription.ID, opts...)
	if err != nil {
		return err
	}

	usages, err = d.SubscriptionUsages(ctx, subscription.ID, opts...)
	if err != nil {
		return err
	}

	planRates, err := d.SubscriptionPlanRates(ctx, subscription.Plan.ID)
	if err != nil {
		return err
	}

	addons, err := d.ListSubscriptionAddons(ctx, subscription.ID, opts...)
	if err != nil {
		return err
	}

	subscription.Plan.QuotaDefaults = defaults
	subscription.Plan.Rates = planRates
	subscription.Quotas = quotas
	subscription.Usages = usages
	subscription.SubscriptionAddons = addons

	return nil
}
