package apitest

import (
	"net/http"
	"strings"
	"testing"
)

// A request body that parses and validates but carries no rows reaches the save
// with an empty slice. GORM's Create rejected that outright at the top level
// (gorm.ErrEmptySlice) while skipping it entirely for an association, so the two
// plan write endpoints answer 500 and creating a plan with neither quota
// defaults nor rates succeeds. Nothing else covers this; the empty list passes
// NewPlanQuotaDefaultList.Validate and NewPlanRateList.Validate.
func TestPlanWritesWithEmptyInput(t *testing.T) {
	resetDB(t)

	t.Cleanup(func() {
		if _, err := testDB.Exec(`DELETE FROM plans WHERE name = $1`, "Empty Plan"); err != nil {
			t.Fatalf("unable to remove the plan this test added: %s", err)
		}
	})

	testCases := []struct {
		name       string
		path       string
		body       string
		wantStatus int
		wantBody   string
	}{
		{
			name:       "no quota defaults in the list",
			path:       "/v1/plans/" + basicPlanID + "/quota-defaults",
			body:       `{}`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   "unable to save the plan quota defaults: empty slice found",
		},
		{
			name:       "no rates in the list",
			path:       "/v1/plans/" + basicPlanID + "/rates",
			body:       `{}`,
			wantStatus: http.StatusInternalServerError,
			wantBody:   "unable to save the plan rates: empty slice found",
		},
		{
			name:       "a new plan with neither quota defaults nor rates",
			path:       "/v1/plans",
			body:       `{"name": "Empty Plan", "description": "a plan with no defaults or rates"}`,
			wantStatus: http.StatusOK,
			wantBody:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := do(t, http.MethodPost, tc.path, tc.body)
			if got.status != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", got.status, tc.wantStatus, got.body)
			}
			if tc.wantBody != "" && !strings.Contains(string(got.body), tc.wantBody) {
				t.Errorf("body = %s, want it to contain %q", got.body, tc.wantBody)
			}
		})
	}
}
