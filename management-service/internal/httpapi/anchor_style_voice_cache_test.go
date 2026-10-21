package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/anchorvoicecache"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
)

type memoryVoiceCache struct {
	reserved, entries []anchorvoicecache.Entry
	publishError      bool
	lookups           int
}

func (c *memoryVoiceCache) NewEntry(t int64, g, id, b string, ttl time.Duration) anchorvoicecache.Entry {
	return anchorvoicecache.New(nil, "fixture").NewEntry(t, g, id, b, ttl)
}
func (c *memoryVoiceCache) Reserve(_ context.Context, e anchorvoicecache.Entry) error {
	c.reserved = append(c.reserved, e)
	return nil
}
func (c *memoryVoiceCache) Publish(_ context.Context, e anchorvoicecache.Entry) error {
	if c.publishError {
		return errors.New("cache publish failed")
	}
	c.entries = append(c.entries, e)
	return nil
}
func (c *memoryVoiceCache) Lookup(_ context.Context, g string) ([]anchorvoicecache.Entry, error) {
	c.lookups++
	out := []anchorvoicecache.Entry{}
	for _, e := range c.entries {
		if e.Group == g && time.Now().Before(e.ExpiresAt) {
			out = append(out, e)
		}
	}
	return out, nil
}

type fixtureCacheReusePolicy struct {
	enabled bool
	err     error
}

func (p *fixtureCacheReusePolicy) GenerationCacheReuseEnabled(context.Context, string) (bool, error) {
	return p.enabled, p.err
}

func TestDisabledMainlineCacheBypassesBothTextAndAudioWithoutDeletingHistory(t *testing.T) {
	s, state, segment, cache := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("ETag", "fixture"); w.WriteHeader(200) })
	e := cache.NewEntry(state.TenantID, cycleVoiceCacheGroup(state, segment), anchorcycle.ID(), "fixture-voice-cache", 5*time.Hour)
	e.Text, e.OutputText = segment.Selected.Text, segment.Selected.Text
	e.Bundle.Text, e.Bundle.AudioDurationMS = cycleVoiceText(segment.Selected), 1000
	cache.entries = []anchorvoicecache.Entry{e}
	policy := &fixtureCacheReusePolicy{}
	s.generationCachePolicy = policy
	if s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{}) != nil {
		t.Fatal("disabled cache reused prose")
	}
	if hit, err := s.cachedCycleVoice(context.Background(), state, segment); err != nil || hit != nil {
		t.Fatal("disabled cache reused audio", hit, err)
	}
	if cache.lookups != 0 || len(cache.entries) != 1 {
		t.Fatal("disabled cache queried or deleted existing content")
	}
	policy.err = errors.New("temporary configuration failure")
	if hit, err := s.cachedCycleVoice(context.Background(), state, segment); err != nil || hit != nil {
		t.Fatal("configuration failure stopped fresh synthesis", err)
	}
	policy.err, policy.enabled = nil, true
	if s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{}) == nil {
		t.Fatal("re-enabling did not restore existing valid prose")
	}
	if hit, err := s.cachedCycleVoice(context.Background(), state, segment); err != nil || hit == nil || !hit.CacheHit {
		t.Fatal("re-enabling did not restore existing audio", hit, err)
	}
}
func (c *memoryVoiceCache) Due(context.Context, int) ([]anchorvoicecache.Entry, error) {
	return c.reserved, nil
}
func (c *memoryVoiceCache) Forget(_ context.Context, e anchorvoicecache.Entry) error {
	c.entries = nil
	c.reserved = nil
	return nil
}

func voiceCacheFixture(t *testing.T, handler http.HandlerFunc) (*Server, *anchorcycle.State, anchorcycle.Segment, *memoryVoiceCache) {
	t.Helper()
	oss := httptest.NewServer(handler)
	t.Cleanup(oss.Close)
	registry, err := storage.NewRegistry(storage.Config{Driver: "local", LocalRoot: t.TempDir(), OSSEndpoint: oss.URL, OSSBucket: "fixture-voice-cache", OSSAccessKeyID: "fixture-id", OSSAccessKeySecret: "fixture-secret"})
	if err != nil {
		t.Fatal(err)
	}
	state := cycleTestState(t)
	if err := installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	state.TenantID = 900001
	state.PlanID = 900002
	state.Config.Threshold = 0
	state.Config.TTS = &anchorcycle.VoiceConfig{Source: "official", VoiceID: "fixture-voice", Model: "fixture-model", Rate: 1, CacheTTLSeconds: 18000}
	state.Phase = "body"
	segment := anchorcycle.Segment{Phase: "body", Task: state.Task(), Selected: anchorcycle.Candidate{ID: "adopted", Text: "哥哥姐姐们，我们家就在这个镇上啊。", FissionSide: state.CurrentFissionSide()}}
	if err := state.CheckP0Candidate(&segment.Selected); err != nil {
		t.Fatal(err)
	}
	cache := &memoryVoiceCache{}
	return &Server{assetStorage: registry, anchorVoiceCache: cache}, &state, segment, cache
}

func TestCycleOSSCacheCompleteBundleHitAndExpiryCleanup(t *testing.T) {
	puts, deletes := []string{}, []string{}
	s, state, segment, cache := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			puts = append(puts, r.URL.Path)
			if r.Header.Get("x-oss-object-acl") != "private" {
				t.Error("cache must be private")
			}
			raw, _ := io.ReadAll(r.Body)
			if len(raw) == 0 {
				t.Error("empty object")
			}
		}
		if r.Method == "DELETE" {
			deletes = append(deletes, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("ETag", "fixture")
		w.WriteHeader(200)
	})
	timeline := []model.LiveAgentPlanTimelineSegment{{SegmentID: "one", Index: 1, StartMS: 0, EndMS: 1000, Text: segment.Selected.Text, SafeCut: true}}
	v, err := s.archiveCycleVoiceCache(context.Background(), state, segment, []byte("fixture-wav"), timeline, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if len(puts) != 5 || len(cache.reserved) != 1 || len(cache.entries) != 1 || v.CacheHit || v.CacheID == "" || v.Bundle.SRT == "" || len(v.Bundle.SafePoints) != 1 {
		t.Fatal("incomplete cache", v, puts)
	}
	if ttl := cache.entries[0].ExpiresAt.Sub(cache.entries[0].CreatedAt); ttl != 5*time.Hour {
		t.Fatal(ttl)
	}
	hit, err := s.cachedCycleVoice(context.Background(), state, segment)
	if err != nil || hit == nil || !hit.CacheHit || hit.Bundle.Text != segment.Selected.Text || len(puts) != 5 {
		t.Fatal("not reused", hit, err)
	}
	segment.Selected.Text = "另一段完全不同的话术。"
	if hit, err = s.cachedCycleVoice(context.Background(), state, segment); err == nil || hit != nil {
		t.Fatal("voice reused for mismatched text", hit, err)
	}
	if err = s.cleanupAnchorVoiceCache(context.Background()); err != nil || len(deletes) > 0 {
		t.Fatal("early deletion", err)
	}
	cache.reserved[0].ExpiresAt = time.Now().Add(-time.Second)
	if err = s.cleanupAnchorVoiceCache(context.Background()); err != nil || len(deletes) != 5 || len(cache.reserved) != 0 {
		t.Fatal("expired bundle not deleted", deletes, err)
	}
	for _, key := range deletes {
		if !strings.Contains(key, "/anchor-voice-cache/v1/") {
			t.Fatal("deleted a non-cache object", key)
		}
	}
}

func TestCycleCacheFingerprintAndContextRules(t *testing.T) {
	s, state, segment, cache := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) { w.Header().Set("ETag", "fixture"); w.WriteHeader(200) })
	group := cycleVoiceCacheGroup(state, segment)
	state.Config.TTS.BufferSeconds = 300
	if cycleVoiceCacheGroup(state, segment) != group {
		t.Fatal("playback buffer altered voice identity")
	}
	state.Config.TTS.CacheTTLSeconds = 36000
	if cycleVoiceCacheGroup(state, segment) != group {
		t.Fatal("policy changes must not invalidate existing expiry-scoped entries")
	}
	state.Facts += "new fact"
	if cycleVoiceCacheGroup(state, segment) == group {
		t.Fatal("changed facts reused cache")
	}
	state.Facts = strings.TrimSuffix(state.Facts, "new fact")
	state.RoomID++
	if cycleVoiceCacheGroup(state, segment) == group {
		t.Fatal("cross-room cache")
	}
	state.RoomID--
	state.Config.TTS.Rate = 1.2
	if cycleVoiceCacheGroup(state, segment) == group {
		t.Fatal("different voice speed reused")
	}
	state.Config.TTS.Rate = 1
	e := cache.NewEntry(state.TenantID, group, anchorcycle.ID(), "fixture-voice-cache", 5*time.Hour)
	e.Text = segment.Selected.Text
	e.OutputText = e.Text
	e.Bundle.AudioDurationMS = 1000
	cache.entries = []anchorvoicecache.Entry{e}
	if s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{}) == nil {
		t.Fatal("angle cache not used")
	}
	state.SegmentDirective = "纠正开头"
	if s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{}) != nil {
		t.Fatal("feedback bypassed")
	}
	state.SegmentDirective = ""
	state.Accepted = []anchorcycle.Segment{segment}
	if s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{}) != nil {
		t.Fatal("recent prose repeated")
	}
	state.Accepted = nil
	state.Phase = "opening"
	if s.cachedCycleCandidate(context.Background(), state, anchorstylescore.Baseline{}) != nil {
		t.Fatal("opening context bypassed")
	}
}

func TestCyclePartialOSSFailureKeepsCleanupAndNeverPublishes(t *testing.T) {
	count := 0
	s, state, segment, cache := voiceCacheFixture(t, func(w http.ResponseWriter, r *http.Request) {
		count++
		if count == 3 {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("ETag", "fixture")
		w.WriteHeader(200)
	})
	timeline := []model.LiveAgentPlanTimelineSegment{{SegmentID: "one", Index: 1, EndMS: 1000, Text: segment.Selected.Text, SafeCut: true}}
	if _, err := s.archiveCycleVoiceCache(context.Background(), state, segment, []byte("fixture"), timeline, 1000); err == nil {
		t.Fatal("upload error ignored")
	}
	if len(cache.reserved) != 1 || len(cache.entries) != 0 {
		t.Fatal("partial objects lost cleanup or were published")
	}
}
