package app

import (
	"context"
	"strings"
	"testing"

	"github.com/cyverse-de/p/go/qms"
)

// A validation failure in addAddon must return the error instead of falling
// through and inserting the add-on anyway. A zero-value App is enough: the
// early return happens before any database access.
func TestAddAddonReturnsOnValidationFailure(t *testing.T) {
	app := &App{}

	// An empty Name fails Addon.Validate() on its first check.
	request := &qms.AddAddonRequest{
		Addon: &qms.Addon{
			Description:   "an add-on with no name",
			DefaultAmount: 1.0,
			ResourceType:  &qms.ResourceType{Name: "cpu.hours", Unit: "cpu hours"},
		},
	}

	response := app.addAddon(context.Background(), request)

	if response.Error == nil {
		t.Fatal("expected a validation error, got nil")
	}
	if !strings.Contains(response.Error.Message, "name must be set") {
		t.Errorf("error message = %q, want it to report the validation failure", response.Error.Message)
	}
}
