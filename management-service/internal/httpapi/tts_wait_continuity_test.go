package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/auth"
	"livecompanion/management/internal/coreclient"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTTSPoolRequiresAuthenticatedAdmin(t *testing.T) {
	s := &Server{auth: auth.NewResolver("test", nil)}
	for _, handler := range []http.HandlerFunc{s.systemTTSPoolGet, s.systemTTSPoolSave} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest("GET", "/api/v1/system/tts-pool", nil))
		if w.Code != 401 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("exposed: %d", w.Code)
		}
	}
}
func TestTTSWaitReusesAcceptedCacheWithStableRetryID(t *testing.T) {
	snapshot := coreclient.ProgramRefreshSnapshot{Running: true, BufferedMainlineMS: 9000}
	var ids []string
	c := &ttsWaitContinuity{snapshot: func(context.Context) (coreclient.ProgramRefreshSnapshot, error) { return snapshot, nil }, append: func(_ context.Context, tr coreclient.ProgramRefreshTrack) error {
		if tr.AudioURL != "https://audio.invalid/approved.wav" || tr.Text != "已审核。" || !tr.AllowConsecutiveRepeat {
			t.Fatal("cache contents changed")
		}
		ids = append(ids, tr.ID)
		if len(ids) == 1 {
			snapshot.BufferedMainlineMS = 45000
			return errors.New("append response lost")
		}
		return nil
	}}
	c.remember(coreclient.ProgramRefreshTrack{ID: "original", AudioURL: "https://audio.invalid/approved.wav", Text: "已审核。", DurationMS: 22000})
	c.wait(t.Context())
	c.wait(t.Context())
	c.wait(t.Context())
	if len(ids) != 2 || ids[0] != ids[1] || ids[0] == "original" || c.pending != nil {
		t.Fatalf("idempotency lost: %v", ids)
	}
	snapshot.BufferedMainlineMS = 5000
	c.wait(t.Context())
	if len(ids) != 3 || ids[2] == ids[1] {
		t.Fatal("next cache replay did not get new delivery ID")
	}
}
func TestTTSWaitDoesNotReplayPausedExpiredOrEmptyCache(t *testing.T) {
	for _, kind := range []string{"empty", "suspended", "stopped", "error", "expired", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			called := false
			c := &ttsWaitContinuity{snapshot: func(context.Context) (coreclient.ProgramRefreshSnapshot, error) {
				if kind == "error" {
					return coreclient.ProgramRefreshSnapshot{}, errors.New("stale owner")
				}
				return coreclient.ProgramRefreshSnapshot{Running: kind != "stopped", Suspended: kind == "suspended"}, nil
			}, append: func(context.Context, coreclient.ProgramRefreshTrack) error { called = true; return nil }}
			url := "https://audio.invalid/approved.wav"
			if kind == "expired" {
				url = "https://audio.invalid/approved.wav?Expires=1"
			}
			if kind != "empty" {
				c.remember(coreclient.ProgramRefreshTrack{AudioURL: url, DurationMS: 1000})
			}
			if kind == "cancelled" {
				cancel()
			}
			c.wait(ctx)
			if called {
				t.Fatal("invalid cached replay")
			}
		})
	}
}
