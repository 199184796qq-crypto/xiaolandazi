package httpapi

import (
	"testing"
	"time"
)

func TestWorkspaceTimeCardPeriod(t *testing.T) {
	// Still December in UTC, already January in the business calendar.
	now := time.Date(2026, 12, 31, 17, 0, 0, 0, time.UTC)
	for _, item := range []struct{ period, from, until string }{
		{"month", "2026-12-31T16:00:00Z", "2027-01-31T16:00:00Z"},
		{"quarter", "2026-12-31T16:00:00Z", "2027-03-31T16:00:00Z"},
		{"year", "2026-12-31T16:00:00Z", "2027-12-31T16:00:00Z"},
	} {
		from, until, ok := workspaceTimeCardPeriod(now, item.period)
		if !ok || from.Format(time.RFC3339) != item.from || until.Format(time.RFC3339) != item.until {
			t.Fatalf("%s: %s to %s, valid=%v", item.period, from, until, ok)
		}
	}
	quarterStart, quarterEnd, ok := workspaceTimeCardPeriod(time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC), "quarter")
	if !ok || quarterStart.Format(time.RFC3339) != "2026-09-30T16:00:00Z" || quarterEnd.Format(time.RFC3339) != "2026-12-31T16:00:00Z" {
		t.Fatalf("fourth quarter: %s to %s, valid=%v", quarterStart, quarterEnd, ok)
	}
	for _, period := range []string{"", "all"} {
		from, until, ok := workspaceTimeCardPeriod(now, period)
		if !ok || !from.IsZero() || !until.IsZero() {
			t.Fatalf("unexpected all-time bounds: %s to %s", from, until)
		}
	}
	if _, _, ok := workspaceTimeCardPeriod(now, "week"); ok {
		t.Fatal("accepted unsupported interval")
	}
}
