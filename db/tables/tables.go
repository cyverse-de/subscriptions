package tables

import (
	"github.com/doug-martin/goqu/v9"

	// Blank-imported for its init(), which registers the "postgres" dialect that db.New asks goqu.New for by name.
	// Co-located with SetDefaultPrepared below rather than left to each binary's entrypoint: prepared mode with the
	// wrong (or no) dialect registered renders "?" placeholders, which lib/pq rejects outright, so the two are one
	// indivisible unit and belong behind the same import every query-building package already has in its closure.
	_ "github.com/doug-martin/goqu/v9/dialect/postgres"
)

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
