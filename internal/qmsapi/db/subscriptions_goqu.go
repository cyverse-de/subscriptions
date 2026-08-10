package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	t "github.com/cyverse-de/subscriptions/db/tables"
	"github.com/cyverse-de/subscriptions/internal/qmsapi/model"
	"github.com/doug-martin/goqu/v9"
)

// The columns backing model.Subscription, model.Quota and model.Usage. goqu fails a scan when a returned column has no
// matching struct field, so the column lists are always spelled out rather than selected with a wildcard. The
// subscription columns are qualified because their query joins users, which has a column of the same name.
var (
	subscriptionColumns = []any{
		t.Subscriptions.Col("id"),
		t.Subscriptions.Col("effective_start_date"),
		t.Subscriptions.Col("effective_end_date"),
		t.Subscriptions.Col("user_id"),
		t.Subscriptions.Col("plan_id"),
		t.Subscriptions.Col("paid"),
		t.Subscriptions.Col("plan_rate_id"),
	}

	quotaColumns = []any{"id", "quota", "subscription_id", "resource_type_id", "last_modified_at"}

	usageColumns = []any{"id", "usage", "subscription_id", "resource_type_id", "last_modified_at"}
)

// activeNowExpression restricts a subscription query to the subscriptions in effect right now, as the database reckons
// it. A subscription with no effective end date remains in effect indefinitely once it has started; every notion of
// "active" in this package has to agree on that, so nothing here builds the predicate by hand.
func activeNowExpression() goqu.Expression {
	return goqu.And(
		t.Subscriptions.Col("effective_start_date").Lte(goqu.L("CURRENT_TIMESTAMP")),
		goqu.Or(
			t.Subscriptions.Col("effective_end_date").IsNull(),
			t.Subscriptions.Col("effective_end_date").Gte(goqu.L("CURRENT_TIMESTAMP")),
		),
	)
}

// notExpiredAtExpression restricts a subscription query to the subscriptions that have not ended as of the given cutoff.
// As with activeNowExpression, a subscription with no effective end date has not ended.
func notExpiredAtExpression(cutoff time.Time) goqu.Expression {
	return goqu.Or(
		t.Subscriptions.Col("effective_end_date").IsNull(),
		t.Subscriptions.Col("effective_end_date").Gte(cutoff),
	)
}

// SubscribeUserToPlan subscribes the given user to the given plan.
func SubscribeUserToPlan(
	ctx context.Context, tx *goqu.TxDatabase, user *model.User, plan *model.Plan, opts *model.SubscriptionOptions,
) (*model.Subscription, error) {
	wrapMsg := "unable to add user plan"

	// Look up the active plan rate.
	planRate, err := plan.GetActivePlanRate()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Define the user plan.
	effectiveStartDate := opts.GetStartDate()
	effectiveEndDate := opts.GetEndDate(effectiveStartDate)
	subscription := model.Subscription{
		EffectiveStartDate: &effectiveStartDate,
		EffectiveEndDate:   &effectiveEndDate,
		UserID:             user.ID,
		PlanID:             plan.ID,
		Quotas:             QuotasFromPlan(plan, opts.GetPeriods()),
		Paid:               opts.IsPaid(),
		PlanRateID:         planRate.ID,
	}

	var subscriptionID string
	found, err := tx.Insert(t.Subscriptions).
		Rows(goqu.Record{
			"effective_start_date": effectiveStartDate,
			"effective_end_date":   effectiveEndDate,
			"user_id":              *user.ID,
			"plan_id":              *plan.ID,
			"paid":                 subscription.Paid,
			"plan_rate_id":         *planRate.ID,
		}).
		Returning("id").
		Executor().
		ScanValContext(ctx, &subscriptionID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: the insert returned no row", wrapMsg)
	}
	subscription.ID = &subscriptionID

	// Save the quotas the plan's defaults imply. GORM created these as an association of the subscription; they are a
	// separate statement here, which is what it emitted anyway.
	if err = saveSubscriptionQuotas(ctx, tx, &subscription); err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return &subscription, nil
}

// savedQuota is one row of a quota insert's RETURNING clause. The resource type comes back alongside the generated
// identifier so that the rows can be matched to the quotas that produced them by value; Postgres does not promise that
// RETURNING emits rows in VALUES order, and a positional match that silently drifted would attach each quota's
// identifier to the wrong resource type with nothing to detect it.
type savedQuota struct {
	ID             string `db:"id"`
	ResourceTypeID string `db:"resource_type_id"`
}

// saveSubscriptionQuotas inserts the quotas belonging to a newly created subscription, recording the generated
// identifiers on the subscription the caller is holding. A subscription has at most one quota per resource type, which
// the unique index on (resource_type_id, subscription_id) enforces, so the resource type identifies a row uniquely.
func saveSubscriptionQuotas(ctx context.Context, tx *goqu.TxDatabase, subscription *model.Subscription) error {
	if len(subscription.Quotas) == 0 {
		return nil
	}

	rows := make([]any, 0, len(subscription.Quotas))
	for _, quota := range subscription.Quotas {
		rows = append(rows, goqu.Record{
			"quota":            quota.Quota,
			"subscription_id":  *subscription.ID,
			"resource_type_id": *quota.ResourceTypeID,
		})
	}

	saved := []savedQuota{}
	err := tx.Insert(t.Quotas).
		Rows(rows...).
		Returning("id", "resource_type_id").
		Executor().
		ScanStructsContext(ctx, &saved)
	if err != nil {
		return fmt.Errorf("unable to save the subscription quotas: %w", err)
	}
	if len(saved) != len(subscription.Quotas) {
		return fmt.Errorf("saved %d of %d subscription quotas", len(saved), len(subscription.Quotas))
	}

	quotaIDs := make(map[string]string, len(saved))
	for _, row := range saved {
		quotaIDs[row.ResourceTypeID] = row.ID
	}
	for i := range subscription.Quotas {
		id, ok := quotaIDs[*subscription.Quotas[i].ResourceTypeID]
		if !ok {
			return fmt.Errorf("no quota was saved for resource type %s", *subscription.Quotas[i].ResourceTypeID)
		}
		subscription.Quotas[i].ID = &id
		subscription.Quotas[i].SubscriptionID = subscription.ID
	}

	return nil
}

// SubscribeUserToDefaultPlan adds the default user plan to the given user.
func SubscribeUserToDefaultPlan(ctx context.Context, tx *goqu.TxDatabase, username string) (*model.Subscription, error) {
	wrapMsg := "unable to add the default user plan"

	// Get the user ID.
	user, err := GetUser(ctx, tx, username)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Get the basic plan ID.
	plan, err := GetPlan(ctx, tx, PlanNameBasic)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Subscribe the user to the plan.
	return SubscribeUserToPlan(ctx, tx, user, plan, &model.SubscriptionOptions{})
}

// GetActiveSubscription retrieves the user plan record that is currently active for the user. The effective start date
// must be before the current date and the effective end date must either be null or after the current date.  If
// multiple active user plans exist, the one with the most recent effective start date is used. If no active user plans
// exist for the user then a new one for the basic plan is created.
func GetActiveSubscription(ctx context.Context, tx *goqu.TxDatabase, username string) (*model.Subscription, error) {
	wrapMsg := "unable to get the active user plan"

	// The identifier breaks ties on the effective start date, which is not unique: without it the row this returns for
	// a user with two subscriptions starting at the same instant would be arbitrary.
	var subscription model.Subscription
	found, err := tx.From(t.Subscriptions).
		Select(subscriptionColumns...).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Where(t.Users.Col("username").Eq(username), activeNowExpression()).
		Order(t.Subscriptions.Col("effective_start_date").Desc(), t.Subscriptions.Col("id").Asc()).
		Limit(1).
		Executor().
		ScanStructContext(ctx, &subscription)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Look up the currently active user plan, adding a new one if it doesn't exist already.
	if !found {
		subPtr, err := SubscribeUserToDefaultPlan(ctx, tx, username)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", wrapMsg, err)
		}
		return subPtr, nil
	}

	return &subscription, nil
}

// HasActiveSubscription determines whether or not the user currently has an active user plan. An absent subscription is
// not an error here: the caller reports it as part of a successful response rather than as a failure.
func HasActiveSubscription(ctx context.Context, tx *goqu.TxDatabase, username string) (bool, error) {
	wrapMsg := "unable to determine whether the user has an active user plan"

	var hasSubscription bool
	_, err := tx.From(t.Subscriptions).
		Select(goqu.L("count(*) > 0")).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Where(t.Users.Col("username").Eq(username), activeNowExpression()).
		Executor().
		ScanValContext(ctx, &hasSubscription)
	if err != nil {
		return false, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return hasSubscription, nil
}

// GetSubscriptionDetails loads the details for the user plan with the given ID from the database. It returns an error
// matching ErrNotFound when no subscription has that identifier.
func GetSubscriptionDetails(ctx context.Context, tx *goqu.TxDatabase, subscriptionID string) (*model.Subscription, error) {
	wrapMsg := fmt.Sprintf("unable to look up subscription '%s'", subscriptionID)

	var subscription model.Subscription
	found, err := tx.From(t.Subscriptions).
		Select(subscriptionColumns...).
		Where(t.Subscriptions.Col("id").Eq(subscriptionID)).
		Executor().
		ScanStructContext(ctx, &subscription)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return nil, fmt.Errorf("%s: %w", wrapMsg, ErrNotFound)
	}

	if err = loadSubscriptionDetails(ctx, tx, &subscription); err != nil {
		return nil, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return &subscription, nil
}

// loadSubscriptionDetails loads the associations required to describe a subscription in a response and to determine its
// plan's active quota defaults. A missing association is left unset rather than reported, matching the GORM preloads
// this replaces; every one of them is a foreign key, so absence means the row was deleted mid-transaction.
func loadSubscriptionDetails(ctx context.Context, tx *goqu.TxDatabase, subscription *model.Subscription) error {
	if subscription.UserID != nil {
		user, err := getUserByID(ctx, tx, *subscription.UserID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			subscription.User = user
		}
	}

	// A missing plan leaves Plan nil, which controllers/usages.go dereferences as subscription.Plan.Name without a
	// guard. That is safe only because subscriptions.plan_id is a NOT NULL foreign key, so the row cannot be absent;
	// anything that relaxes the constraint has to give the consumers a nil check first.
	if subscription.PlanID != nil {
		plan, err := GetPlanByID(ctx, tx, *subscription.PlanID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			subscription.Plan = plan
		}
	}

	if subscription.PlanRateID != nil {
		planRate, err := getPlanRateByID(ctx, tx, *subscription.PlanRateID)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
		if err == nil {
			subscription.PlanRate = planRate
		}
	}

	if err := loadSubscriptionQuotas(ctx, tx, subscription); err != nil {
		return err
	}

	return loadSubscriptionUsages(ctx, tx, subscription)
}

// loadSubscriptionQuotas loads the quotas recorded against a subscription, along with the resource type each one
// applies to. The listing is deliberately unordered: the query it replaces had no ORDER BY.
func loadSubscriptionQuotas(ctx context.Context, tx *goqu.TxDatabase, subscription *model.Subscription) error {
	// Initialized rather than declared nil so that a subscription with no quotas marshals as [] and not null: goqu only
	// touches the destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	quotas := []model.Quota{}
	err := tx.From(t.Quotas).
		Select(quotaColumns...).
		Where(goqu.C("subscription_id").Eq(*subscription.ID)).
		Executor().
		ScanStructsContext(ctx, &quotas)
	if err != nil {
		return fmt.Errorf("unable to look up the subscription quotas: %w", err)
	}

	resourceTypeIDs := make([]string, 0, len(quotas))
	for _, quota := range quotas {
		resourceTypeIDs = append(resourceTypeIDs, *quota.ResourceTypeID)
	}
	resourceTypes, err := resourceTypesByID(ctx, tx, resourceTypeIDs)
	if err != nil {
		return err
	}
	for i := range quotas {
		if resourceType, ok := resourceTypes[*quotas[i].ResourceTypeID]; ok {
			quotas[i].ResourceType = *resourceType
		}
	}
	subscription.Quotas = quotas

	return nil
}

// loadSubscriptionUsages loads the usage amounts recorded against a subscription, along with the resource type each one
// applies to. The listing is deliberately unordered: the query it replaces had no ORDER BY.
func loadSubscriptionUsages(ctx context.Context, tx *goqu.TxDatabase, subscription *model.Subscription) error {
	// Initialized for the same reason as the quota listing above; a user who has recorded no usage is routine, and this
	// slice is what GET /v1/usages/{username} returns.
	usages := []model.Usage{}
	err := tx.From(t.Usages).
		Select(usageColumns...).
		Where(goqu.C("subscription_id").Eq(*subscription.ID)).
		Executor().
		ScanStructsContext(ctx, &usages)
	if err != nil {
		return fmt.Errorf("unable to look up the subscription usages: %w", err)
	}

	resourceTypeIDs := make([]string, 0, len(usages))
	for _, usage := range usages {
		resourceTypeIDs = append(resourceTypeIDs, *usage.ResourceTypeID)
	}
	resourceTypes, err := resourceTypesByID(ctx, tx, resourceTypeIDs)
	if err != nil {
		return err
	}
	for i := range usages {
		if resourceType, ok := resourceTypes[*usages[i].ResourceTypeID]; ok {
			usages[i].ResourceType = *resourceType
		}
	}
	subscription.Usages = usages

	return nil
}

// GetActiveSubscriptionDetails retrieves the user plan information that is currently active for the user, subscribing
// the user to the basic plan if no active subscription exists. This function is like GetActiveSubscription except that
// it also loads all of the user plan details from the database.
func GetActiveSubscriptionDetails(ctx context.Context, tx *goqu.TxDatabase, username string) (*model.Subscription, error) {
	subscription, err := GetActiveSubscription(ctx, tx, username)
	if err != nil {
		return nil, err
	}

	return GetSubscriptionDetails(ctx, tx, *subscription.ID)
}

// ListSubscriptionsForUser lists subscriptions for a single user, along with the total number of them, which is the same
// number unless the listing is paginated. Expired subscriptions are omitted unless includeExpired is set, where expiry
// is reckoned against the given cutoff rather than the current time.
func ListSubscriptionsForUser(
	ctx context.Context, tx *goqu.TxDatabase, username string, includeExpired bool, cutoff time.Time,
) ([]*model.Subscription, int64, error) {
	wrapMsg := fmt.Sprintf("unable to list the subscriptions for user '%s'", username)

	conditions := []goqu.Expression{t.Users.Col("username").Eq(username)}
	if !includeExpired {
		conditions = append(conditions, notExpiredAtExpression(cutoff))
	}

	var count int64
	_, err := tx.From(t.Subscriptions).
		Select(goqu.COUNT(goqu.Star())).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Where(conditions...).
		Executor().
		ScanValContext(ctx, &count)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Initialized rather than declared nil so that a user with no subscriptions marshals as [] and not null: goqu only
	// touches the destination once per row, where GORM's Find replaced it with an empty slice before reading any.
	subscriptions := []*model.Subscription{}
	err = tx.From(t.Subscriptions).
		Select(subscriptionColumns...).
		Join(t.Users, goqu.On(t.Subscriptions.Col("user_id").Eq(t.Users.Col("id")))).
		Where(conditions...).
		Order(
			t.Subscriptions.Col("effective_start_date").Asc(),
			t.Subscriptions.Col("effective_end_date").Asc(),
		).
		Executor().
		ScanStructsContext(ctx, &subscriptions)
	if err != nil {
		return nil, 0, fmt.Errorf("%s: %w", wrapMsg, err)
	}

	for _, subscription := range subscriptions {
		if err = loadSubscriptionDetails(ctx, tx, subscription); err != nil {
			return nil, 0, fmt.Errorf("%s: %w", wrapMsg, err)
		}
	}

	return subscriptions, count, nil
}

// DeactivateSubscriptions marks subscriptions for a user as expired. This operation is used when a user subscribes to a
// new plan. Subscriptions with no effective end date are treated as running indefinitely, matching activeNowExpression;
// closing them here is what keeps a new subscription from running alongside an open-ended one.
func DeactivateSubscriptions(
	ctx context.Context, tx *goqu.TxDatabase, userID string, startDate, endDate time.Time,
) error {
	wrapMsg := "unable to deactivate active plans for user"

	// Subscriptions that should be marked as inactive as of the start date.
	_, err := tx.Update(t.Subscriptions).
		Set(goqu.Record{"effective_end_date": startDate}).
		Where(
			goqu.C("user_id").Eq(userID),
			goqu.C("effective_start_date").Lte(startDate),
			goqu.Or(
				goqu.C("effective_end_date").IsNull(),
				goqu.C("effective_end_date").Gt(startDate),
			),
		).
		Executor().
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Subscriptions that should become effective as of the end date. The upper bound on the start date keeps
	// subscriptions scheduled entirely after the new window from being dragged backwards into it.
	_, err = tx.Update(t.Subscriptions).
		Set(goqu.Record{"effective_start_date": endDate}).
		Where(
			goqu.C("user_id").Eq(userID),
			goqu.C("effective_start_date").Gte(startDate),
			goqu.C("effective_start_date").Lt(endDate),
			goqu.Or(
				goqu.C("effective_end_date").IsNull(),
				goqu.C("effective_end_date").Gt(endDate),
			),
		).
		Executor().
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}

	// Subscriptions that should never become effective.
	_, err = tx.Update(t.Subscriptions).
		Set(goqu.Record{"effective_end_date": goqu.C("effective_start_date")}).
		Where(
			goqu.C("user_id").Eq(userID),
			goqu.C("effective_start_date").Gte(startDate),
			goqu.C("effective_end_date").Lte(endDate),
		).
		Executor().
		ExecContext(ctx)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}

	return nil
}

// UpsertQuota updates a quota if a corresponding quota exists in the database. If a corresponding quota does not exist,
// a new quota will be inserted.
func UpsertQuota(ctx context.Context, tx *goqu.TxDatabase, quota *model.Quota) error {
	wrapMsg := "unable to insert or update the quota"

	// The conflict target is the unique index on (resource_type_id, subscription_id): without it a repeated quota update
	// for the same subscription and resource type would insert a second row instead of replacing the limit. Only the
	// quota value is reassigned, matching the clause this replaces; the other two columns are what the row conflicted on.
	var id string
	found, err := tx.Insert(t.Quotas).
		Rows(goqu.Record{
			"quota":            quota.Quota,
			"subscription_id":  *quota.SubscriptionID,
			"resource_type_id": *quota.ResourceTypeID,
		}).
		OnConflict(goqu.DoUpdate("subscription_id,resource_type_id", goqu.Record{
			"quota": goqu.I("excluded.quota"),
		})).
		Returning("id").
		Executor().
		ScanValContext(ctx, &id)
	if err != nil {
		return fmt.Errorf("%s: %w", wrapMsg, err)
	}
	if !found {
		return fmt.Errorf("%s: the upsert returned no row", wrapMsg)
	}
	quota.ID = &id

	return nil
}
