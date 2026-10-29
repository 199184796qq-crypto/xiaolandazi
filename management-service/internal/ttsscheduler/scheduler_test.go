package ttsscheduler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRollingQuotaBoundariesAndSmoothing(t *testing.T) {
	now := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	var full []time.Time
	for i := 0; i < 180; i++ {
		full = append(full, now.Add(-30*time.Second))
	}
	for _, tc := range []struct {
		name     string
		at       []time.Time
		rpm, rps int
		want     time.Duration
	}{
		{"empty", nil, 180, 3, 0},
		{"minute full", full, 180, 3, 30*time.Second + time.Millisecond},
		{"one second full", []time.Time{now.Add(-900 * time.Millisecond), now.Add(-700 * time.Millisecond), now.Add(-500 * time.Millisecond)}, 180, 3, 101 * time.Millisecond},
		{"exact boundary", []time.Time{now.Add(-time.Minute)}, 1, 3, 0},
		{"smooth", []time.Time{now}, 180, 3, time.Second/3 + time.Millisecond},
		{"admin lower", []time.Time{now.Add(-2 * time.Second)}, 1, 1, 58*time.Second + time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := WaitForQuota(now, tc.at, tc.rpm, tc.rps); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	// Drive many simulated starts and check every rolling window independently.
	var admitted []time.Time
	for i := 0; i < 1000; i++ {
		if wait := WaitForQuota(now, admitted, 180, 3); wait > 0 {
			now = now.Add(wait)
		}
		nMinute, nSecond := 0, 0
		for _, at := range admitted {
			if at.After(now.Add(-time.Minute)) {
				nMinute++
			}
			if at.After(now.Add(-time.Second)) {
				nSecond++
			}
		}
		if nMinute >= 180 || nSecond >= 3 {
			t.Fatalf("over admission at %d: %d/%d", i, nMinute, nSecond)
		}
		admitted = append(admitted, now)
	}
}

type queueFixture struct {
	mu      sync.Mutex
	works   []Work
	granted bool
	status  string
	claimed chan struct{}
	once    sync.Once
}

func (f *queueFixture) EnqueueTTS(_ context.Context, w Work) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.works = append(f.works, w)
	return nil
}
func (f *queueFixture) ClaimTTS(context.Context, string) (Claim, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.once.Do(func() { close(f.claimed) })
	return Claim{Granted: f.granted, Wait: time.Millisecond, Credential: Credential{AccountID: "second", APIKey: "never-serialize", VoiceID: "mapped"}}, nil
}
func (f *queueFixture) FinishTTS(ctx context.Context, _, status string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
	return nil
}
func TestQueuedCancellationKeepsHookAndNoGrant(t *testing.T) {
	app, stop := context.WithCancel(t.Context())
	defer stop()
	repo := &queueFixture{claimed: make(chan struct{})}
	service := New(app, repo)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	hook := make(chan struct{}, 1)
	ctx = WithWaitHook(ctx, func(context.Context) {
		select {
		case hook <- struct{}{}:
		default:
		}
	})
	go func() { _, err := service.Acquire(ctx, Work{Model: "m", VoiceID: "v"}); done <- err }()
	select {
	case <-hook:
	case <-time.After(time.Second):
		t.Fatal("mainline did not retain cached supply hook")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.status != "cancelled" || len(repo.works) != 1 {
		t.Fatalf("queue not removed: %+v", repo)
	}
}
func TestServiceShutdownAndPermitCredential(t *testing.T) {
	t.Run("shutdown", func(t *testing.T) {
		app, stop := context.WithCancel(t.Context())
		repo := &queueFixture{claimed: make(chan struct{})}
		done := make(chan error, 1)
		go func() { _, err := New(app, repo).Acquire(t.Context(), Work{Model: "m"}); done <- err }()
		<-repo.claimed
		stop()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
	t.Run("grant", func(t *testing.T) {
		repo := &queueFixture{granted: true, claimed: make(chan struct{})}
		permit, err := New(t.Context(), repo).Acquire(t.Context(), Work{Model: "m"})
		if err != nil || permit.AccountID != "second" || permit.VoiceID != "mapped" {
			t.Fatal("wrong account permit", err)
		}
		raw, _ := json.Marshal(permit)
		if strings.Contains(string(raw), "never-serialize") {
			t.Fatal("credential exposed")
		}
		permit.Finish("completed")
		if repo.status != "completed" {
			t.Fatal("not completed")
		}
	})
}
