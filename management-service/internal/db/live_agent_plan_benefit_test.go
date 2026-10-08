package db

import (
	"testing"
	"time"
)

func TestNormalizeBenefitValidityMode(t *testing.T) {
	for input, want := range map[string]string{
		"":          "scheduled",
		"scheduled": "scheduled",
		"PERMANENT": "permanent",
	} {
		got, err := normalizeBenefitValidityMode(input)
		if err != nil || got != want {
			t.Fatalf("normalizeBenefitValidityMode(%q)=(%q,%v), want %q", input, got, err, want)
		}
	}
	if _, err := normalizeBenefitValidityMode("forever"); err == nil {
		t.Fatal("invalid validity mode should be rejected")
	}
}

func TestBenefitStatusForWindowPermanentIgnoresStoredTimes(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	future := now.Add(time.Hour)
	past := now.Add(-time.Hour)
	if got := benefitStatusForWindow("permanent", &future, &past, now); got != "active" {
		t.Fatalf("permanent status=%q, want active", got)
	}
	if got := benefitStatusForWindow("scheduled", nil, nil, now); got != "draft" {
		t.Fatalf("incomplete scheduled status=%q, want draft", got)
	}
	if got := benefitStatusForWindow("scheduled", &past, &past, now); got != "expired" {
		t.Fatalf("past scheduled status=%q, want expired", got)
	}
}
