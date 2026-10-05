package db

import (
	"errors"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

func TestLiveContentRefreshTimingAndLease(t *testing.T) {
	base := time.Date(2026, 10, 4, 10, 0, 0, 0, time.UTC)
	p := model.DefaultLiveContentPolicy()
	p.ContentMode = model.LiveContentAIDynamic
	for _, newer := range []bool{false, true} {
		c := model.LiveContentRefreshCandidate{StartedAt: base, PublishedAt: base.Add(-time.Hour)}
		if newer {
			c.PublishedAt = base.Add(time.Hour)
		}
		prepare, apply := refreshTimes(c, p)
		anchor := c.StartedAt
		if newer {
			anchor = c.PublishedAt
		}
		if prepare != anchor.Add(110*time.Minute) || apply != anchor.Add(2*time.Hour) || p.ReplacementPercent != 25 {
			t.Fatalf("incorrect two-hour schedule: %v %v policy=%+v", prepare, apply, p)
		}
	}
	until := base.Add(time.Minute)
	j := model.LiveContentRefreshJob{Status: "running", LeaseToken: "owner", LeaseUntil: &until}
	if err := validateRefreshLease(j, "owner", base); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		token string
		now   time.Time
	}{{"reclaimed", base}, {"", base}, {"owner", until}, {"owner", until.Add(time.Second)}} {
		if err := validateRefreshLease(j, tc.token, tc.now); !errors.Is(err, ErrLiveContentRefreshLeaseLost) {
			t.Fatalf("stale worker accepted: %+v err=%v", tc, err)
		}
	}
	j.Status = "cancelled"
	if err := validateRefreshLease(j, "owner", base); !errors.Is(err, ErrLiveContentRefreshLeaseLost) {
		t.Fatal("cancelled worker can still publish")
	}
}
