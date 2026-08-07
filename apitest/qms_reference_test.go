package apitest

import (
	"net/http"
	"testing"
)

// The plan and resource-type listings are backed entirely by data the
// migrations seed with fixed UUIDs, so these responses are fully deterministic.
// Terrain proxies all three to the DE UI.
func TestReferenceDataEndpoints(t *testing.T) {
	resetDB(t)

	testCases := []struct {
		name       string
		golden     string
		path       string
		wantStatus int
	}{
		{
			name:       "list plans",
			golden:     "plans_list",
			path:       "/v1/plans",
			wantStatus: http.StatusOK,
		},
		{
			name:       "get plan by ID",
			golden:     "plans_get_basic",
			path:       "/v1/plans/99e47c22-950a-11ec-84a4-406c8f3e9cbb",
			wantStatus: http.StatusOK,
		},
		{
			name:       "list resource types",
			golden:     "resource_types_list",
			path:       "/v1/resource-types",
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assertGolden(t, tc.golden, do(t, http.MethodGet, tc.path, ""), tc.wantStatus)
		})
	}
}
