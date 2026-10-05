package collector

import (
	"testing"
	"time"
)

func TestOfflineConfirmationRequiresFullSixtySecondWindow(t *testing.T) {
	now := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)
	suspectedAt, confirmed := nextOfflineConfirmation(time.Time{}, now)
	if suspectedAt.IsZero() || confirmed {
		t.Fatalf("first offline: suspected_at=%v confirmed=%v", suspectedAt, confirmed)
	}

	suspectedAt, confirmed = nextOfflineConfirmation(suspectedAt, now.Add(59*time.Second))
	if confirmed {
		t.Fatalf("59 seconds must remain reconnecting: suspected_at=%v", suspectedAt)
	}

	_, confirmed = nextOfflineConfirmation(suspectedAt, now.Add(60*time.Second))
	if !confirmed {
		t.Fatal("60 seconds without live evidence must confirm offline")
	}
}

func TestOfflineConfirmationRestartsAfterLiveEvidenceReset(t *testing.T) {
	now := time.Date(2026, 9, 30, 17, 0, 0, 0, time.UTC)
	first, _ := nextOfflineConfirmation(time.Time{}, now)

	// Manager resets the timestamp to zero whenever live evidence returns.
	restarted, confirmed := nextOfflineConfirmation(time.Time{}, now.Add(30*time.Second))
	if confirmed || restarted.Equal(first) {
		t.Fatalf("restarted window=%v first=%v confirmed=%v", restarted, first, confirmed)
	}
}
