// Package ttsscheduler coordinates TTS admission across rooms and processes.
// Text and provider credentials never enter the durable queue.
package ttsscheduler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

const CurrentAccountID = "current-aliyun-tts"
const MaxRPM = 180
const MaxRPS = 3
const MaxQueue = 1000

var ErrRevision = errors.New("TTS账户配置已变化，请刷新后重试")
var ErrQueueFull = errors.New("TTS请求队列已满，请稍后重试")
var ErrExpired = errors.New("TTS排队请求已取消或过期")
var ErrConfig = errors.New("TTS账户配置无效")

type Scope struct {
	TenantID  int64 `json:"tenant_id"`
	RoomID    int64 `json:"room_id"`
	SessionID int64 `json:"session_id"`
}
type Work struct {
	ID       string `json:"id"`
	Model    string `json:"model"`
	Provider string `json:"provider"`
	VoiceID  string `json:"voice_id"`
	Scope
	TTL time.Duration `json:"-"`
}
type Claim struct {
	Granted    bool
	Wait       time.Duration
	Credential Credential
}

// Credential only crosses the in-process admission boundary. Never serialize it.
type Credential struct {
	AccountID string `json:"-"`
	APIKey    string `json:"-"`
	VoiceID   string `json:"-"`
}
type Permit struct {
	Credential
	Finish func(string) `json:"-"`
}
type waitHookKey struct{}

// WithWaitHook lets mainline supply reuse already approved audio while waiting.
// Interaction requests deliberately have no hook and cannot interrupt playback.
func WithWaitHook(ctx context.Context, hook func(context.Context)) context.Context {
	return context.WithValue(ctx, waitHookKey{}, hook)
}

type Repository interface {
	EnqueueTTS(context.Context, Work) error
	ClaimTTS(context.Context, string) (Claim, error)
	FinishTTS(context.Context, string, string) error
}
type Service struct {
	ctx  context.Context
	repo Repository
}

func New(ctx context.Context, repo Repository) *Service { return &Service{ctx: ctx, repo: repo} }

// Acquire waits without occupying a provider connection or reserving a fee.
// A grant consumes rate allowance until its window expires, even on failure.
func (s *Service) Acquire(parent context.Context, work Work) (Permit, error) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(s.ctx, cancel)
	defer func() { stop(); cancel() }()
	if err := ctx.Err(); err != nil {
		return Permit{}, err
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Permit{}, err
	}
	work.ID = "tts-" + hex.EncodeToString(id[:])
	work.TTL = 5 * time.Minute
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < work.TTL {
		work.TTL = time.Until(deadline)
	}
	if err := s.repo.EnqueueTTS(ctx, work); err != nil {
		return Permit{}, err
	}
	finish := func(status string) {
		settle, done := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
		defer done()
		_ = s.repo.FinishTTS(settle, work.ID, status)
	}
	granted := false
	defer func() {
		if !granted {
			finish("cancelled")
		}
	}()
	var lastHook time.Time
	for {
		if err := ctx.Err(); err != nil {
			return Permit{}, err
		}
		claim, err := s.repo.ClaimTTS(ctx, work.ID)
		if err != nil {
			return Permit{}, err
		}
		if claim.Granted {
			if err := ctx.Err(); err != nil {
				return Permit{}, err
			}
			granted = true
			return Permit{Credential: claim.Credential, Finish: finish}, nil
		}
		if hook, ok := parent.Value(waitHookKey{}).(func(context.Context)); ok && time.Since(lastHook) >= time.Second {
			lastHook = time.Now()
			hookCtx, done := context.WithTimeout(ctx, 2*time.Second)
			hook(hookCtx)
			done()
		}
		wait := min(max(claim.Wait, 25*time.Millisecond), 250*time.Millisecond)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Permit{}, ctx.Err()
		case <-timer.C:
		}
	}
}

// WaitForQuota computes both sliding windows and smooths starts. In-flight
// completion does not release a rate slot; only the passage of time does.
func WaitForQuota(now time.Time, admitted []time.Time, rpm, rps int) time.Duration {
	if rpm < 1 || rpm > MaxRPM || rps < 1 || rps > MaxRPS {
		return time.Minute
	}
	var minute, second []time.Time
	var latest time.Time
	for _, at := range admitted {
		if at.After(latest) {
			latest = at
		}
		if at.After(now.Add(-time.Minute)) {
			minute = append(minute, at)
		}
		if at.After(now.Add(-time.Second)) {
			second = append(second, at)
		}
	}
	oldest := func(times []time.Time) time.Time {
		value := times[0]
		for _, at := range times[1:] {
			if at.Before(value) {
				value = at
			}
		}
		return value
	}
	var wait time.Duration
	if len(minute) >= rpm {
		wait = max(wait, oldest(minute).Add(time.Minute+time.Millisecond).Sub(now))
	}
	if len(second) >= rps {
		wait = max(wait, oldest(second).Add(time.Second+time.Millisecond).Sub(now))
	}
	if !latest.IsZero() {
		wait = max(wait, latest.Add(time.Second/time.Duration(rps)+time.Millisecond).Sub(now))
	}
	return max(wait, 0)
}

type Account struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	Provider         string            `json:"provider"`
	CredentialSource string            `json:"credential_source"`
	Configured       bool              `json:"configured"`
	Enabled          bool              `json:"enabled"`
	RPM              int               `json:"rpm"`
	RPS              int               `json:"rps"`
	Revision         uint64            `json:"revision"`
	UsedMinute       int               `json:"used_minute"`
	UsedSecond       int               `json:"used_second"`
	InFlight         int               `json:"in_flight"`
	AccountUID       string            `json:"account_uid"`
	Voices           map[string]string `json:"voices"`
}
type AccountInput struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Enabled    bool              `json:"enabled"`
	RPM        int               `json:"rpm"`
	RPS        int               `json:"rps"`
	Revision   uint64            `json:"revision"`
	AccountUID string            `json:"account_uid"`
	NewAPIKey  string            `json:"new_api_key,omitempty"`
	Voices     map[string]string `json:"voices"`
}
type Queued struct {
	Work
	QueuedAt  time.Time `json:"queued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}
type Snapshot struct {
	Accounts   []Account `json:"accounts"`
	Queued     int       `json:"queued"`
	Requests   []Queued  `json:"requests"`
	ObservedAt time.Time `json:"observed_at"`
}
