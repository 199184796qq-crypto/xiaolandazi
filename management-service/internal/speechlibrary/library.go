// Package speechlibrary owns durable room-scoped approved text and complete
// audio bundles. It never restores or controls a livestream's progress.
package speechlibrary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

const SettingKey = "mainline_speech_library_policy"

var ErrStale = errors.New("话术库请求属于旧场次或已变化的内容")

type Policy struct {
	Capacity        int `json:"capacity"`
	OneRate         int `json:"one_rate"`
	TwoRate         int `json:"two_rate"`
	FullRate        int `json:"full_rate"`
	CooldownSeconds int `json:"cooldown_seconds"`
}

func DefaultPolicy() Policy { return Policy{3, 30, 60, 70, 180} }
func (p Policy) Validate() error {
	if p.Capacity < 1 || p.Capacity > 20 || p.OneRate < 0 || p.OneRate > p.TwoRate || p.TwoRate > p.FullRate || p.FullRate > 100 || p.CooldownSeconds < 0 || p.CooldownSeconds > 86400 {
		return errors.New("话术库容量须为1～20，复用率须递增且在0～100之间，重复间隔须为0～86400秒")
	}
	return nil
}
func (p Policy) Rate(n int) int {
	if n < 1 {
		return 0
	}
	if n == 1 {
		return p.OneRate
	}
	if n == 2 {
		return p.TwoRate
	}
	return p.FullRate
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func ReuseDecision(key string, n int, p Policy) bool {
	h := sha256.Sum256([]byte(key))
	v := uint32(h[0])<<24 | uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	return int(v%100) < p.Rate(n)
}

type Scope struct {
	Realm                                        string
	Tenant, Plan, Room, Runtime                  int64
	Slot, TextContext, VoiceContext, RoomContext string
}

func (s Scope) Key() string {
	return Digest(struct {
		Realm              string
		Tenant, Plan, Room int64
		Slot               string
	}{s.Realm, s.Tenant, s.Plan, s.Room, s.Slot})
}
func (s Scope) Validate() error {
	if s.Realm == "" || s.Tenant <= 0 || s.Plan <= 0 || s.Room <= 0 || s.Runtime <= 0 || s.Slot == "" || len(s.TextContext) != 64 || len(s.VoiceContext) != 64 || len(s.RoomContext) != 64 {
		return errors.New("话术库范围或指纹无效")
	}
	return nil
}
func ObjectPrefix(s Scope, id string) string {
	return fmt.Sprintf("mainline-speech-library/v1/%s/t%d/r%d/p%d/%s/", Digest(s.Realm)[:16], s.Tenant, s.Room, s.Plan, id)
}
func ObjectKeys(prefix string) []string {
	return []string{prefix + "audio.wav", prefix + "subtitle.srt", prefix + "timeline.json", prefix + "safe-points.json", prefix + "manifest.json"}
}

type Entry struct {
	ID         string                `json:"id"`
	Candidate  anchorcycle.Candidate `json:"candidate"`
	Admission  *Admission            `json:"admission,omitempty"`
	LastUsedMS int64                 `json:"last_used_ms"`
	UseCount   int64                 `json:"use_count"`
}
type Audio struct {
	ID, EntryID, Bucket, Prefix, VoiceContext string
	Bundle                                    model.LiveAgentPlanVersionVariant
}
type Choice struct {
	Entry           *Entry
	TrackID, Reason string
}
type Stats struct{ Texts, Audio, PlayedReuses int64 }
type Repository interface {
	Select(context.Context, Scope, Policy, string, string) (Choice, error)
	Cancel(context.Context, string) error
	ReserveAudio(context.Context, Scope, string, string, Audio) error
	Publish(context.Context, Scope, Policy, string, string, Entry, Audio) error
	ClaimAudio(context.Context, Scope, string, string) (*Audio, error)
	MarkPlayed(context.Context, int64, int64, int64, string, time.Time) error
	ReleaseRuntime(context.Context, int64, int64, int64) error
	Due(context.Context, int) ([]Audio, error)
	ForgetAudio(context.Context, string) error
	Statistics(context.Context) (Stats, error)
}
