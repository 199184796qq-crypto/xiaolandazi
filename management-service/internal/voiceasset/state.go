// Package voiceasset owns cache invalidation identity. It never calls a model,
// starts playback, or treats a missing supplier receipt as a refund.
package voiceasset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var ErrCleared = errors.New("声音已清除，旧生成或缓存不可再次使用")
var ErrBusy = errors.New("声音仍有播放或共享引用")
var ErrConflict = errors.New("清理范围已变化，请刷新预览")

type Scope struct {
	Tenant   int64  `json:"tenant_id"`
	Plan     int64  `json:"plan_id"`
	Category string `json:"category"`
	Voice    string `json:"voice_key,omitempty"`
}

func (s Scope) Validate() error {
	if s.Tenant <= 0 || s.Plan <= 0 || (s.Category != "mainline" && s.Category != "interaction") {
		return errors.New("声音清理归属无效")
	}
	if s.Voice != "" && !DigestValid(s.Voice) {
		return errors.New("声音身份无效")
	}
	return nil
}
func DigestValid(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && s == strings.ToLower(s)
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

type Stamp struct {
	Scope      Scope  `json:"scope"`
	PlanEpoch  uint64 `json:"plan_epoch"`
	VoiceEpoch uint64 `json:"voice_epoch"`
}

func (s Stamp) Key(legacy string) string {
	if s.PlanEpoch == 0 && s.VoiceEpoch == 0 {
		return legacy
	}
	return Digest([]any{"voice-cache-epoch-v1", s.Scope, s.PlanEpoch, s.VoiceEpoch, legacy})
}
func (s Stamp) Equal(t Stamp) bool        { return s == t }
func (s Stamp) Metadata(m map[string]any) { m["voice_cache_stamp"] = s }

type stampKey struct{}
type guardKey struct{}

func WithStamp(ctx context.Context, s Stamp, check func(context.Context, Stamp) error) context.Context {
	ctx = context.WithValue(ctx, stampKey{}, s)
	return context.WithValue(ctx, guardKey{}, func(c context.Context) error { return check(c, s) })
}
func FromContext(ctx context.Context) (Stamp, bool) {
	s, ok := ctx.Value(stampKey{}).(Stamp)
	return s, ok
}
func Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if f, ok := ctx.Value(guardKey{}).(func(context.Context) error); ok {
		return f(ctx)
	}
	return nil
}

type EpochReader interface {
	VoiceCacheStamp(context.Context, Scope) (Stamp, error)
	CheckVoiceCacheStamp(context.Context, Stamp) error
}

type readerScope struct {
	Scope  Scope
	Reader EpochReader
}
type readerKey struct{}

func WithReader(ctx context.Context, scope Scope, r EpochReader) context.Context {
	return context.WithValue(ctx, readerKey{}, readerScope{scope, r})
}
func Bind(ctx context.Context, voice string) (context.Context, error) {
	if _, ok := FromContext(ctx); ok {
		return ctx, Check(ctx)
	}
	x, ok := ctx.Value(readerKey{}).(readerScope)
	if !ok {
		return ctx, nil
	}
	x.Scope.Voice = voice
	st, e := x.Reader.VoiceCacheStamp(ctx, x.Scope)
	if e != nil {
		return ctx, e
	}
	return WithStamp(ctx, st, x.Reader.CheckVoiceCacheStamp), nil
}

// Legacy classification is deliberately narrow. An absent/invalid owner is
// review-required, never an excuse to delete the whole generated_voice prefix.
func ScopeFromMetadata(tenant int64, assetType string, m map[string]any) (Scope, bool) {
	if assetType != "generated_voice" {
		return Scope{}, false
	}
	if raw, ok := m["voice_cache_stamp"]; ok {
		var st Stamp
		b, e := json.Marshal(raw)
		if e != nil || json.Unmarshal(b, &st) != nil || st.Scope.Validate() != nil || st.Scope.Tenant != tenant {
			return Scope{}, false
		}
		if p, exists := m["plan_id"]; exists && fmt.Sprint(p) != strconv.FormatInt(st.Scope.Plan, 10) {
			return Scope{}, false
		}
		return st.Scope, true
	}
	plan, e := strconv.ParseInt(fmt.Sprint(m["plan_id"]), 10, 64)
	if e != nil || plan <= 0 {
		return Scope{}, false
	}
	purpose, _ := m["purpose"].(string)
	if purpose != "live_agent_formal_voice_timeline" {
		return Scope{}, false
	}
	return Scope{Tenant: tenant, Plan: plan, Category: "mainline"}, true
}
func MetadataStamp(tenant int64, assetType string, m map[string]any) (Stamp, bool) {
	scope, ok := ScopeFromMetadata(tenant, assetType, m)
	if !ok {
		return Stamp{}, false
	}
	if raw, ok := m["voice_cache_stamp"]; ok {
		var s Stamp
		b, _ := json.Marshal(raw)
		if json.Unmarshal(b, &s) != nil {
			return Stamp{}, false
		}
		return s, true
	}
	return Stamp{Scope: scope}, true
}

type ClearRequest struct {
	Mode      string `json:"mode,omitempty"`
	Scope     Scope  `json:"scope"`
	RequestID string `json:"request_id"`
	Expected  Stamp  `json:"expected"`
	Actor     int64  `json:"-"`
}

func (r ClearRequest) Validate() error {
	if r.Mode != "" && r.Mode != "history" && r.Mode != "local_only" {
		return ErrConflict
	}
	if e := r.Scope.Validate(); e != nil {
		return e
	}
	if !DigestValid(r.RequestID) || r.Expected.Scope != r.Scope {
		return ErrConflict
	}
	return nil
}

type Job struct {
	Mode      string           `json:"mode"`
	ID        string           `json:"id"`
	Scope     Scope            `json:"scope"`
	Before    Stamp            `json:"before"`
	After     Stamp            `json:"after"`
	State     string           `json:"state"`
	ScanAfter int64            `json:"scan_after"`
	ScanDone  bool             `json:"scan_done"`
	Created   time.Time        `json:"created_at"`
	Counts    map[string]int64 `json:"counts"`
}
type Candidate struct {
	Asset    int64     `json:"asset_id"`
	Driver   string    `json:"driver"`
	Bucket   string    `json:"bucket"`
	Key      string    `json:"-"`
	Bytes    uint64    `json:"bytes"`
	Stamp    Stamp     `json:"stamp"`
	State    string    `json:"state"`
	Attempts int       `json:"attempts"`
	Next     time.Time `json:"next_at"`
}

func RetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 9 {
		attempt = 9
	}
	d := time.Second * time.Duration(1<<uint(attempt))
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

// Cancellation and GC failures never turn an invalid generation active again.
func Eligible(stored, current Stamp) bool { return stored.Equal(current) }

var SidecarFields = []string{"subtitle_object_key", "timeline_object_key", "safe_points_object_key", "manifest_object_key"}
