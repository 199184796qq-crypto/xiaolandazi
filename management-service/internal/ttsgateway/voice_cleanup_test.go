package ttsgateway

import (
	"context"
	"errors"
	"livecompanion/management/internal/voiceasset"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type cleanupEpochFixture struct{ epoch atomic.Uint64 }

func (f *cleanupEpochFixture) VoiceCacheStamp(_ context.Context, scope voiceasset.Scope) (voiceasset.Stamp, error) {
	return voiceasset.Stamp{Scope: scope, PlanEpoch: f.epoch.Load()}, nil
}
func (f *cleanupEpochFixture) CheckVoiceCacheStamp(ctx context.Context, st voiceasset.Stamp) error {
	now, _ := f.VoiceCacheStamp(ctx, st.Scope)
	if now != st {
		return voiceasset.ErrCleared
	}
	return nil
}
func TestVoiceCleanupInteractionOldAudioNeverRestoredAfterClear(t *testing.T) {
	f := newReuseFixture(t)
	epochs := &cleanupEpochFixture{}
	g := f.gateway()
	g.interactionAudioCache.WithEpochReader(epochs)
	ctx := func() context.Context {
		return voiceasset.WithReader(f.ctx(14, 4), voiceasset.Scope{Tenant: 14, Plan: 4, Category: "interaction"}, epochs)
	}
	for i := 0; i < 2; i++ {
		a, e := g.SynthesizeURL(ctx(), reuseRequest())
		if e != nil || a.AudioReused != (i == 1) {
			t.Fatal(a, e)
		}
	}
	epochs.epoch.Add(1)
	// A new gateway simulates restart; stale on-disk entries must not revive.
	g = f.gateway()
	g.interactionAudioCache.WithEpochReader(epochs)
	a, e := g.SynthesizeURL(ctx(), reuseRequest())
	if e != nil || a.AudioReused || f.paid.Load() != 2 {
		t.Fatal("cleared audio revived", a, e, f.paid.Load())
	}
	g.interactionAudioCache.prune(t.Context())
	files, e := filepath.Glob(filepath.Join(g.interactionAudioCache.root, "*.json"))
	if e != nil || len(files) != 1 {
		t.Fatal("old metadata not collected", files, e)
	}
}
func TestVoiceCleanupLatePaidReceiptSettlesButDoesNotActivate(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "reuse_enabled", false: "reuse_disabled"}[enabled], func(t *testing.T) {
			f := newReuseFixture(t)
			f.policy.Enabled = enabled
			release := make(chan struct{})
			f.wait = release
			epochs := &cleanupEpochFixture{}
			g := f.gateway()
			ctx := voiceasset.WithReader(f.ctx(14, 4), voiceasset.Scope{Tenant: 14, Plan: 4, Category: "interaction"}, epochs)
			result := make(chan error, 1)
			go func() { _, e := g.SynthesizeURL(ctx, reuseRequest()); result <- e }()
			deadline := time.Now().Add(3 * time.Second)
			for f.paid.Load() == 0 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if f.paid.Load() != 1 {
				close(release)
				t.Fatal("fixture never submitted")
			}
			epochs.epoch.Add(1)
			close(release)
			select {
			case e := <-result:
				if !errors.Is(e, voiceasset.ErrCleared) {
					t.Fatal("late audio accepted", e)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("late receipt blocked")
			}
			if f.after.Load() != 1 || f.before.Load() != 1 {
				t.Fatal("paid receipt lost", f.before.Load(), f.after.Load())
			}
			entries, _ := os.ReadDir(g.interactionAudioCache.root)
			for _, entry := range entries {
				if filepath.Ext(entry.Name()) == ".json" {
					t.Fatal("late result entered cache")
				}
			}
		})
	}
}

func TestVoiceCleanupScopedInteractionMetadataKeepsOtherPlan(t *testing.T) {
	c := NewInteractionAudioCache(t.TempDir(), "prod").WithEpochReader(&cleanupEpochFixture{})
	now := time.Now().UTC()
	scope := voiceasset.Scope{Tenant: 14, Plan: 4, Category: "interaction", Voice: voiceasset.Digest("complete identity")}
	for i := 0; i < 2; i++ {
		st := voiceasset.Stamp{Scope: scope}
		if i == 1 {
			st.Scope.Plan = 5
		}
		key := voiceasset.Digest(i)
		entry := interactionAudioEntry{Version: 1, Key: key, Stamp: &st, CreatedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Audio: SynthesizeResponse{VoiceID: "fixture", AudioURL: "https://supplier.invalid/owned-by-supplier"}}
		if e := writeInteractionAudioEntry(filepath.Join(c.root, key+".json"), entry); e != nil {
			t.Fatal(e)
		}
	}
	voices, e := c.ScopedVoices(t.Context(), scope)
	if e != nil || len(voices) != 1 {
		t.Fatal(voices, e)
	}
	if e = c.ClearScopedFiles(t.Context(), voiceasset.Job{Scope: scope, Mode: "local_only", Created: now}); e != nil {
		t.Fatal(e)
	}
	files, e := filepath.Glob(filepath.Join(c.root, "*.json"))
	if e != nil || len(files) != 1 {
		t.Fatal("other plan metadata removed", files, e)
	}
}
