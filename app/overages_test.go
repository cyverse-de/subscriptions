package app

import (
	"context"
	"testing"

	"github.com/cyverse-de/p/go/qms"
)

// New must carry the --report-overages flag through to the field the overage
// paths check. It previously hardcoded true, so passing false did nothing.
func TestNewCarriesReportOverages(t *testing.T) {
	for _, reportOverages := range []bool{true, false} {
		app := New(nil, "example.org", reportOverages)
		if app.ReportOverages != reportOverages {
			t.Errorf("New(..., %t).ReportOverages = %t", reportOverages, app.ReportOverages)
		}
	}
}

// With reporting off, both overage paths return before building a database
// client, so a nil handle is enough to prove the short circuit is live.
func TestOveragesDisabledShortCircuits(t *testing.T) {
	app := New(nil, "example.org", false)
	ctx := context.Background()

	overages := app.getUserOverages(ctx, &qms.AllUserOveragesRequest{Username: "someuser"})
	if overages.Error != nil {
		t.Fatalf("getUserOverages returned an error: %s", overages.Error.Message)
	}
	if len(overages.Overages) != 0 {
		t.Errorf("got %d overages, want none", len(overages.Overages))
	}

	isOverage := app.checkUserOverages(ctx, &qms.IsOverageRequest{
		Username:     "someuser",
		ResourceName: "data.size",
	})
	if isOverage.Error != nil {
		t.Fatalf("checkUserOverages returned an error: %s", isOverage.Error.Message)
	}
	if isOverage.IsOverage {
		t.Error("IsOverage = true, want false when reporting is disabled")
	}
}
