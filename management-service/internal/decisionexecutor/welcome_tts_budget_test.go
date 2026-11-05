package decisionexecutor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/ttsgateway"
)

func TestWelcomePreparationEstimateIsConservativeAndBounded(t *testing.T) {
	w := New(readyVoiceStore(), &fakeCore{}, &fakeAgent{}, &fakeTTS{})
	if w.estimateTTPreparation("欢迎") != 5*time.Second || w.estimateTTPreparation(strings.Repeat("字", 200)) != 9*time.Second {
		t.Fatal("cold estimate ignores input")
	}
	w.observeTTPreparation(8 * time.Second)
	if w.estimateTTPreparation("欢迎") != 10*time.Second {
		t.Fatal("recent provider tail ignored")
	}
	for i := 0; i < 40; i++ {
		w.observeTTPreparation(time.Millisecond)
	}
	if w.estimateTTPreparation("欢迎") != 10*time.Second {
		t.Fatal("instant/cache response erased estimate")
	}
	for i := 0; i < 40; i++ {
		w.observeTTPreparation(time.Second)
	}
	if len(w.ttsPreparationSamples) != 32 || w.estimateTTPreparation("欢迎") != 5*time.Second || w.estimateTTPreparation(strings.Repeat("字", 2000)) != 35*time.Second {
		t.Fatal("unbounded estimator")
	}
}

type deadlineAwareTTS struct {
	fakeTTS
	deadline time.Time
}

func (p *deadlineAwareTTS) SynthesizeURL(ctx context.Context, req ttsgateway.SynthesizeRequest) (ttsgateway.SynthesizeResponse, error) {
	p.deadline, _ = ctx.Deadline()
	return p.fakeTTS.SynthesizeURL(ctx, req)
}

func TestWelcomePurchaseBoundaryRechecksAfterWaitAndEveryFallback(t *testing.T) {
	w, _, _, _, _, _, e := v2Fixture(t, 52001)
	now := time.Now()
	w.now = func() time.Time { return now }
	namesUntil := now.Add(20 * time.Second)
	raw, _ := json.Marshal(e)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["welcome_quota"] = true
	m["welcome_names_expires_at"] = namesUntil
	raw, _ = json.Marshal(m)
	item := decisionItem{ID: "welcome", PurposeKnowledge: raw, ExpiresAt: e.ExpiresAt}
	provider := &deadlineAwareTTS{}
	w.tts = provider
	request := ttsgateway.SynthesizeRequest{Text: "欢迎新来的朋友。"}
	if _, err := w.synthesizeInteractionAudio(context.Background(), &item, request); err != nil || provider.calls != 1 || !provider.deadline.Equal(namesUntil.Add(-time.Second)) {
		t.Fatal("fresh request rejected or not bounded", err, provider.calls, provider.deadline)
	}
	now = now.Add(16 * time.Second)
	for _, stage := range []string{"interaction_tts", "interaction_fact_refresh_tts"} {
		if _, err := w.synthesizeInteractionAudioStage(context.Background(), &item, stage, request); !errors.Is(err, errWelcomeTTSTimeInsufficient) {
			t.Fatal("expired purchase authorized", stage, err)
		}
	}
	if provider.calls != 1 {
		t.Fatal("fallback bought duplicate after wait", provider.calls)
	}
	// Ready audio and ordinary questions have their own admission rules.
	m["welcome_ready_at"] = now
	item.PurposeKnowledge, _ = json.Marshal(m)
	if until, err := w.welcomeTTSPurchaseDeadline(item, request.Text); err != nil || !until.IsZero() {
		t.Fatal("ready lease confused with names", err)
	}
	m["welcome_quota"] = false
	delete(m, "welcome_ready_at")
	item.PurposeKnowledge, _ = json.Marshal(m)
	if _, err := w.welcomeTTSPurchaseDeadline(item, request.Text); err != nil {
		t.Fatal("non-welcome affected", err)
	}
	m["welcome_quota"] = true
	delete(m, "welcome_names_expires_at")
	item.PurposeKnowledge, _ = json.Marshal(m)
	if _, err := w.welcomeTTSPurchaseDeadline(item, request.Text); !errors.Is(err, errWelcomeTTSTimeInsufficient) {
		t.Fatal("missing contract authorized purchase", err)
	}
}

type welcomeBudgetCore struct {
	*fakeCore
	prep        int64
	reason      string
	allow       bool
	advance     func()
	withdrawals int
}

func (c *welcomeBudgetCore) DoRoom(ctx context.Context, tenant, room int64, method, path string, q url.Values, body any) (*http.Response, error) {
	if strings.HasSuffix(path, "/preflight") {
		request := body.(map[string]any)
		if request["stage"] == "tts" {
			c.prep, _ = request["preparation_ms"].(int64)
			if c.allow {
				if c.advance != nil {
					c.advance()
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"allowed":true}`))}, nil
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"allowed":false,"reason":"welcome_tts_time_insufficient"}`))}, nil
		}
	}
	if strings.HasSuffix(path, "/release") {
		c.reason = q.Get("block_reason")
	}
	if method == http.MethodDelete {
		c.withdrawals++
	}
	return c.fakeCore.DoRoom(ctx, tenant, room, method, path, q, body)
}

func TestWelcomePreflightDenialNeverCallsTTSAndReleasesWithCause(t *testing.T) {
	w, _, core, agent, tts, session, e := v2Fixture(t, 52002)
	e.Body = "欢迎新来的朋友。"
	e.Intent.Question = false
	raw, _ := json.Marshal(e)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["welcome_quota"] = true
	m["welcome_names_expires_at"] = time.Now().Add(2 * time.Second)
	claim, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "welcome", "mission_kind": "welcome_named", "purpose_knowledge": m, "expires_at": e.ExpiresAt}})
	core.claimRaw = string(claim)
	gate := &welcomeBudgetCore{fakeCore: core}
	w.core = gate
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if gate.prep < 5000 || gate.reason != "welcome_tts_time_insufficient" || tts.calls != 0 || agent.calls != 0 || core.dispatches != 0 {
		t.Fatal("paid work despite denial", gate, tts.calls, agent.calls, core.dispatches)
	}
}

// Ensure the final guard also covers time consumed after Core allowed preflight.
func TestWelcomeGuardHonorsEarlierSelectionDeadline(t *testing.T) {
	w, _, _, _, _, _, e := v2Fixture(t, 52003)
	now := time.Now()
	w.now = func() time.Time { return now }
	raw, _ := json.Marshal(e)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["welcome_quota"] = true
	m["welcome_names_expires_at"] = now.Add(25 * time.Second)
	raw, _ = json.Marshal(m)
	item := decisionItem{PurposeKnowledge: raw, ExpiresAt: now.Add(4 * time.Second)}
	if _, err := w.welcomeTTSPurchaseDeadline(item, "欢迎"); !errors.Is(err, errWelcomeTTSTimeInsufficient) {
		t.Fatal("selection expired first", err)
	}
}

func TestWelcomeTimeSpentAfterAllowedPreflightIsReleasedWithoutPurchaseOrManualCancel(t *testing.T) {
	w, _, core, agent, tts, session, e := v2Fixture(t, 52004)
	now := time.Now()
	w.now = func() time.Time { return now }
	e.Body = "欢迎新来的朋友。"
	e.Intent.Question = false
	raw, _ := json.Marshal(e)
	var m map[string]any
	json.Unmarshal(raw, &m)
	m["welcome_quota"] = true
	m["welcome_names_expires_at"] = now.Add(8 * time.Second)
	claim, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "welcome", "mission_kind": "welcome_named", "purpose_knowledge": m, "expires_at": e.ExpiresAt}})
	core.claimRaw = string(claim)
	gate := &welcomeBudgetCore{fakeCore: core, allow: true, advance: func() { now = now.Add(4 * time.Second) }}
	w.core = gate
	if err := w.processRoom(context.Background(), session); !errors.Is(err, errWelcomeTTSTimeInsufficient) {
		t.Fatal("actual purchase guard failed", err)
	}
	if tts.calls != 0 || agent.calls != 0 || core.dispatches != 0 || core.releases != 1 || gate.reason != "welcome_tts_time_insufficient" || gate.withdrawals != 0 {
		t.Fatal("guard bought or manually cancelled", tts.calls, agent.calls, core.releases, gate)
	}
}
