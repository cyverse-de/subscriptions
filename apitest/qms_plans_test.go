package apitest

import (
	"net/http"
	"testing"
)

// apitest/plans_test.go declares a function-local basicPlanID with the same
// value inside TestAddPlanQuotaDefaults. That shadows this one rather than
// colliding with it, so both compile — but remove the local one as part of this
// task so there is a single declaration in the package.
const (
	basicPlanID   = "99e47c22-950a-11ec-84a4-406c8f3e9cbb"
	unknownPlanID = "00000000-0000-0000-0000-000000000000"
)

// The active rate and active quota defaults are computed from migration-seeded
// reference data, so these responses are fully deterministic. Both pick the most
// recent row whose effective_date has already passed.
func TestActivePlanRateAndQuotaDefaults(t *testing.T) {
	resetDB(t)

	testCases := []struct {
		name       string
		golden     string
		path       string
		wantStatus int
	}{
		{
			name:       "active rate",
			golden:     "v1_plan_active_rate",
			path:       "/v1/plans/" + basicPlanID + "/active-rate",
			wantStatus: http.StatusOK,
		},
		{
			name:       "active quota defaults",
			golden:     "v1_plan_active_quota_defaults",
			path:       "/v1/plans/" + basicPlanID + "/active-quota-defaults",
			wantStatus: http.StatusOK,
		},
		{
			name:       "active rate for an unknown plan",
			golden:     "v1_plan_active_rate_unknown",
			path:       "/v1/plans/" + unknownPlanID + "/active-rate",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "active quota defaults for an unknown plan",
			golden:     "v1_plan_active_quota_defaults_unknown",
			path:       "/v1/plans/" + unknownPlanID + "/active-quota-defaults",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertGolden(t, tc.golden, do(t, http.MethodGet, tc.path, ""), tc.wantStatus)
		})
	}
}
