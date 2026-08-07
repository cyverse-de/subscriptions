package apitest

import (
	"fmt"
	"net/http"
	"testing"
)

// The add-on routes are terrain's, via clients/qms_addons.clj. The existing
// unit tests cover the nil-body guards without a database; these cover the
// round trip terrain actually performs.

// addonBody renders the AddAddonRequest body terrain sends.
func addonBody(name, description string, amount float64, rate float64) string {
	return fmt.Sprintf(
		`{"addon": {"name": %q, "description": %q, "default_amount": %v,`+
			`"resource_type": {"name": "cpu.hours", "unit": "cpu hours"},`+
			`"addon_rates": [{"rate": %v, "effective_date": %q}]}}`,
		name, description, amount, rate, effectiveDate,
	)
}

// createAddon adds an add-on and returns its UUID.
func createAddon(t *testing.T, name string) string {
	t.Helper()

	got := do(t, http.MethodPut, "/addons", addonBody(name, "an add-on", 100, 5))
	if got.status != http.StatusOK {
		t.Fatalf("unable to create the add-on: status %d, body %s", got.status, got.body)
	}

	addon, ok := mustDecode(t, got)["addon"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}
	uuid, _ := addon["uuid"].(string)
	if uuid == "" {
		t.Fatalf("the created add-on has no uuid: %s", got.body)
	}
	return uuid
}

// subscriptionIDFor returns the active subscription's UUID for a user.
func subscriptionIDFor(t *testing.T, username string) string {
	t.Helper()
	return queryString(t, `
		SELECT s.id::text
		  FROM subscriptions s
		  JOIN users u ON s.user_id = u.id
		 WHERE u.username = $1`, username)
}

func TestAddonLifecycle(t *testing.T) {
	resetDB(t)

	t.Run("create", func(t *testing.T) {
		got := do(t, http.MethodPut, "/addons", addonBody("Extra CPU", "an add-on", 100, 5))
		assertGolden(t, "addon_created", got, http.StatusOK)
	})

	t.Run("list", func(t *testing.T) {
		got := do(t, http.MethodGet, "/addons", "")
		assertGolden(t, "addons_listed", got, http.StatusOK)
	})
}

// Attaching an add-on to a subscription is what terrain does when an admin
// grants extra resources.
func TestSubscriptionAddons(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")

	addonID := createAddon(t, "Extra CPU")
	subscriptionID := subscriptionIDFor(t, trimmedUser)

	// The UUID that identifies the attachment, which is not the add-on's.
	var subscriptionAddonID string

	t.Run("attach it to the subscription", func(t *testing.T) {
		path := fmt.Sprintf("/subscriptions/%s/addons/%s", subscriptionID, addonID)
		got := do(t, http.MethodPut, path, "")
		assertGolden(t, "subscription_addon_added", got, http.StatusOK)

		attached, ok := mustDecode(t, got)["subscription_addon"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected response shape: %s", got.body)
		}
		subscriptionAddonID, _ = attached["uuid"].(string)
		if subscriptionAddonID == "" {
			t.Fatalf("the attachment has no uuid: %s", got.body)
		}
	})

	t.Run("list the subscription's add-ons", func(t *testing.T) {
		got := do(t, http.MethodGet, fmt.Sprintf("/subscriptions/%s/addons", subscriptionID), "")
		assertGolden(t, "subscription_addons_listed", got, http.StatusOK)
	})

	// The :addon_uuid path segment is misleadingly named: the read and delete
	// handlers pass it straight to a lookup on subscription_addons.uuid, so it
	// has to be the attachment's UUID and the :sub_uuid segment is ignored
	// entirely. Terrain works around this by passing the same UUID twice.
	// Pinned because a merge that "fixes" the naming would break terrain.
	t.Run("the add-on's own UUID is not accepted", func(t *testing.T) {
		path := fmt.Sprintf("/subscriptions/%s/addons/%s", subscriptionID, addonID)
		assertGolden(t, "subscription_addon_wrong_uuid", do(t, http.MethodGet, path, ""), http.StatusNotFound)
	})

	t.Run("remove it", func(t *testing.T) {
		path := fmt.Sprintf("/subscriptions/%s/addons/%s", subscriptionID, subscriptionAddonID)
		got := do(t, http.MethodDelete, path, "")
		if got.status != http.StatusOK {
			t.Fatalf("status %d, body %s", got.status, got.body)
		}

		remaining := queryInt(t, `SELECT count(*) FROM subscription_addons WHERE subscription_id = $1`, subscriptionID)
		if remaining != 0 {
			t.Errorf("subscription_addons rows = %d, want 0 after the delete", remaining)
		}
	})
}
