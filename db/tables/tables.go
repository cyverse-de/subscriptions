package tables

import "github.com/doug-martin/goqu/v9"

var (
	UpdateOperations  = goqu.T("update_operations")
	UOps              = UpdateOperations
	Users             = goqu.T("users")
	UserPlans         = goqu.T("user_plans")
	Plans             = goqu.T("plans")
	PlanQuotaDefaults = goqu.T("plan_quota_defaults")
	PQD               = PlanQuotaDefaults
	ResourceTypes     = goqu.T("resource_types")
	RT                = ResourceTypes
	Quotas            = goqu.T("quotas")
	Usages            = goqu.T("usages")
	Updates           = goqu.T("updates")
)
