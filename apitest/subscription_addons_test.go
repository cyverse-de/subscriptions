package apitest

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// attachAddon attaches an add-on to a subscription and returns the attachment's
// own UUID, which is what the /subscription-addons routes take.
func attachAddon(t *testing.T, subscriptionID, addonID string) string {
	t.Helper()

	got := do(t, http.MethodPut, fmt.Sprintf("/subscriptions/%s/addons/%s", subscriptionID, addonID), "")
	if got.status != http.StatusOK {
		t.Fatalf("unable to attach the add-on: status %d, body %s", got.status, got.body)
	}

	attached, ok := mustDecode(t, got)["subscription_addon"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected response shape: %s", got.body)
	}
	uuid, _ := attached["uuid"].(string)
	if uuid == "" {
		t.Fatalf("the attachment has no uuid: %s", got.body)
	}
	return uuid
}

// /subscription-addons/{uuid} replaces the nested routes, whose :addon_uuid
// segment is a subscription add-on's UUID rather than an add-on's and whose
// :sub_uuid segment is ignored. These take the one UUID they actually use.
func TestSubscriptionAddonsByID(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	subscriptionID := subscriptionIDFor(t, trimmedUser)

	t.Run("read one", func(t *testing.T) {
		subAddonID := attachAddon(t, subscriptionID, createAddon(t, "Read Me"))

		got := do(t, http.MethodGet, "/subscription-addons/"+subAddonID, "")
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}

		read, ok := mustDecode(t, got)["subscription_addon"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected response shape: %s", got.body)
		}
		if read["uuid"] != subAddonID {
			t.Errorf("uuid = %v, want %s", read["uuid"], subAddonID)
		}
	})

	t.Run("update one", func(t *testing.T) {
		subAddonID := attachAddon(t, subscriptionID, createAddon(t, "Update Me"))

		body := fmt.Sprintf(
			`{"subscription_addon": {"uuid": %q, "amount": 42, "paid": false},`+
				`"update_amount": true, "update_paid": true}`, subAddonID)
		got := do(t, http.MethodPost, "/subscription-addons/"+subAddonID, body)
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}

		if amount := queryFloat(t,
			`SELECT amount FROM subscription_addons WHERE id = $1`, subAddonID); amount != 42 {
			t.Errorf("amount = %v, want 42", amount)
		}
	})

	// The path names the row on this route, so a body pointing somewhere else
	// must not reach through to it. The nested route it replaces had no path to
	// disagree with, which is the whole reason for these routes.
	t.Run("the path wins over the body", func(t *testing.T) {
		target := attachAddon(t, subscriptionID, createAddon(t, "Target"))
		bystander := attachAddon(t, subscriptionID, createAddon(t, "Bystander"))

		body := fmt.Sprintf(
			`{"subscription_addon": {"uuid": %q, "amount": 99, "paid": false},`+
				`"update_amount": true, "update_paid": true}`, bystander)
		got := do(t, http.MethodPost, "/subscription-addons/"+target, body)
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}

		if amount := queryFloat(t,
			`SELECT amount FROM subscription_addons WHERE id = $1`, target); amount != 99 {
			t.Errorf("the path-named attachment has amount %v, want 99", amount)
		}
		if amount := queryFloat(t,
			`SELECT amount FROM subscription_addons WHERE id = $1`, bystander); amount == 99 {
			t.Errorf("the body-named attachment was updated as well")
		}
	})

	t.Run("delete one", func(t *testing.T) {
		subAddonID := attachAddon(t, subscriptionID, createAddon(t, "Delete Me"))

		got := do(t, http.MethodDelete, "/subscription-addons/"+subAddonID, "")
		if got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}

		if rows := queryInt(t,
			`SELECT count(*) FROM subscription_addons WHERE id = $1`, subAddonID); rows != 0 {
			t.Errorf("subscription_addons rows = %d, want 0 after the delete", rows)
		}
	})

	t.Run("a malformed uuid is refused", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodDelete} {
			got := do(t, method, "/subscription-addons/not-a-uuid", "")
			if got.status != http.StatusBadRequest {
				t.Errorf("%s status = %d, want %d; body: %s", method, got.status, http.StatusBadRequest, got.body)
			}
		}
	})
}

// The nested routes still carry terrain, so they have to keep working until it
// moves over.
func TestDeprecatedSubscriptionAddonRoutesStillWork(t *testing.T) {
	resetDB(t)
	subscribeUser(t, trimmedUser, "Basic")
	subscriptionID := subscriptionIDFor(t, trimmedUser)
	subAddonID := attachAddon(t, subscriptionID, createAddon(t, "Legacy"))

	// Terrain passes the attachment's UUID for both segments, which is what the
	// handlers require: the first is ignored and the second is looked up in
	// subscription_addons.
	path := fmt.Sprintf("/subscriptions/%s/addons/%s", subAddonID, subAddonID)

	logged := captureLogs(t, func() {
		if got := do(t, http.MethodGet, path, ""); got.status != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
		}
	})
	if !strings.Contains(logged, "deprecated route called") {
		t.Errorf("the deprecated route did not log a warning:\n%s", logged)
	}

	if got := do(t, http.MethodDelete, path, ""); got.status != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", got.status, http.StatusOK, got.body)
	}
	if rows := queryInt(t,
		`SELECT count(*) FROM subscription_addons WHERE id = $1`, subAddonID); rows != 0 {
		t.Errorf("subscription_addons rows = %d, want 0 after the delete", rows)
	}
}
