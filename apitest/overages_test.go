package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// The overage routes gate analysis launches: app-exposer calls
// GET /users/{username}/overages/{resource_name} before starting work and
// refuses to launch when the answer is yes. The existing unit tests only cover
// the short circuit taken when overage reporting is switched off, so these are
// the first tests of the behavior the service actually ships with.

// A user inside their quota is not in overage.
func TestOveragesWithinQuota(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 10)

	t.Run("the overage list is empty", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages", "")
		assertGolden(t, "overages_none", got, http.StatusOK)
	})

	t.Run("the single-resource check says no", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages/cpu.hours", "")
		assertGolden(t, "overage_check_false", got, http.StatusOK)
	})
}

// A user past their quota is in overage, and the resource is named so the
// caller can report which limit was hit.
func TestOveragesOverQuota(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	setQuota(t, trimmedUser, "cpu.hours", 100)
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 150)

	t.Run("the resource is listed", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages", "")
		assertGolden(t, "overages_cpu_hours", got, http.StatusOK)
	})

	t.Run("the single-resource check says yes", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages/cpu.hours", "")
		assertGolden(t, "overage_check_true", got, http.StatusOK)
	})

	// A resource the user is under on must not inherit the other one's answer.
	t.Run("an unaffected resource is still clear", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages/data.size", "")
		assertGolden(t, "overage_check_other_resource", got, http.StatusOK)
	})
}

// Consuming exactly the allocation already counts as an overage, so a user at
// 100/100 cpu.hours is refused rather than allowed one last launch. Recorded
// because it's a boundary a merge could flip without anyone noticing, and
// because it's worth confirming the inclusive comparison is deliberate.
func TestOverageIncludesExactlyAtQuota(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	setQuota(t, trimmedUser, "cpu.hours", 100)
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 100)

	got := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages/cpu.hours", "")
	assertGolden(t, "overage_check_at_quota", got, http.StatusOK)
}

// The suffix is stripped from the path, so app-exposer can pass either form.
func TestOveragesStripTheUsernameSuffix(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	setQuota(t, trimmedUser, "cpu.hours", 100)
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 150)

	bare := do(t, http.MethodGet, "/users/"+trimmedUser+"/overages/cpu.hours", "")
	suffixed := do(t, http.MethodGet, "/users/"+testUser+"/overages/cpu.hours", "")
	if string(bare.body) != string(suffixed.body) {
		t.Errorf("the two username forms disagree:\n bare: %s\n full: %s", bare.body, suffixed.body)
	}
}

// A user this service has never seen has no subscription and therefore no
// quota. Recorded because it decides whether an unknown user can launch work:
// app-exposer treats "not in overage" as permission to proceed.
func TestOveragesForAnUnknownUser(t *testing.T) {
	resetDB(t)

	t.Run("the overage list", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/nosuchuser/overages", "")
		assertGolden(t, "overages_unknown_user", got, http.StatusOK)
	})

	t.Run("the single-resource check", func(t *testing.T) {
		got := do(t, http.MethodGet, "/users/nosuchuser/overages/cpu.hours", "")
		assertGolden(t, "overage_check_unknown_user", got, http.StatusOK)
	})
}

// GET /users/{username}/usages is what data-usage-api reads back.
func TestGetUsages(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	recordUsage(t, trimmedUser, "cpu.hours", "cpu hours", "SET", 42)

	got := do(t, http.MethodGet, fmt.Sprintf("/users/%s/usages", trimmedUser), "")
	assertGolden(t, "usages_after_set", got, http.StatusOK)
}
