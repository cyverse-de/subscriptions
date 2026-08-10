package tables

import "github.com/doug-martin/goqu/v9"

// goqu renders literal values into the SQL text unless prepared mode is on, and
// its escaping (doubling single quotes) is only complete while PostgreSQL's
// standard_conforming_strings is on. Binding parameters instead removes that
// dependency on a server setting this service does not control.
//
// This lives here rather than in main() because it has to take effect for the
// test binaries too, and every package that builds a goqu query in this repo
// imports this one for its table identifiers — so package initialization order
// guarantees the setting is in place before the first query is built.
func init() {
	goqu.SetDefaultPrepared(true)
}

var (
	UpdateOperations   = goqu.T("update_operations")
	UOps               = UpdateOperations
	Users              = goqu.T("users")
	Subscriptions      = goqu.T("subscriptions")
	SubscriptionAddons = goqu.T("subscription_addons")
	Plans              = goqu.T("plans")
	PlanQuotaDefaults  = goqu.T("plan_quota_defaults")
	PQD                = PlanQuotaDefaults
	ResourceTypes      = goqu.T("resource_types")
	RT                 = ResourceTypes
	Quotas             = goqu.T("quotas")
	Usages             = goqu.T("usages")
	Updates            = goqu.T("updates")
	Addons             = goqu.T("addons")
	PlanRates          = goqu.T("plan_rates")
	AddonRates         = goqu.T("addon_rates")
)
