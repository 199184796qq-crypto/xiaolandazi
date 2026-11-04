package httpapi

import (
	"testing"
	"time"
)

func TestVoiceAutomaticPolicyPreservesUnknownAndStrongReferences(t *testing.T) {
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	ago := func(days int) *time.Time { v := now.Add(-time.Duration(days) * 24 * time.Hour); return &v }
	for _, tc := range []struct {
		last, retired *time.Time
		strong        bool
		want          string
	}{
		{nil, nil, false, "usage_unknown_review"}, {ago(90), nil, true, "protected_reference"},
		{ago(29), nil, false, "recently_used"}, {ago(30), nil, false, "idle_observation"}, {ago(36), nil, false, "idle_observation"}, {ago(37), nil, false, "idle_candidate"},
		{nil, ago(6), false, "retired_observation"}, {nil, ago(7), false, "retired_candidate"},
	} {
		if got := voiceAutomaticCandidate(now, tc.last, tc.strong, tc.retired); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
