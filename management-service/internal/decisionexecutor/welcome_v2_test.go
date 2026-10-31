package decisionexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type welcomeCoreFixture struct {
	*fakeCore
	worker         *Worker
	tts            *fakeTTS
	knowledge      map[string]any
	ready          time.Time
	admissions     int
	readyBeforeTTS bool
}

func (c *welcomeCoreFixture) DoRoom(ctx context.Context, tenant, room int64, method, path string, q url.Values, body any) (*http.Response, error) {
	if !strings.HasSuffix(path, "/welcome-ready") {
		return c.fakeCore.DoRoom(ctx, tenant, room, method, path, q, body)
	}
	c.admissions++
	if c.tts.calls < 1 {
		c.readyBeforeTTS = true
	}
	if c.ready.IsZero() {
		c.ready = c.worker.now()
	}
	c.knowledge["welcome_ready_at"] = c.ready
	c.knowledge["expires_at"] = c.ready.Add(30 * time.Second)
	raw, _ := json.Marshal(map[string]any{"knowledge": c.knowledge, "expires_at": c.ready.Add(30 * time.Second)})
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
}

func TestWelcomeWorkerOneWholeTTSForOneAndTenPeople(t *testing.T) {
	for _, n := range []int{1, 10} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			w, _, core, agent, tts, session, e := v2Fixture(t, int64(50000+n))
			e.Body = "欢迎新来的朋友，一起看看。"
			e.Intent.Question = false
			raw, _ := json.Marshal(e)
			var knowledge map[string]any
			json.Unmarshal(raw, &knowledge)
			knowledge["welcome_quota"] = true
			names := make([]string, n)
			for i := range names {
				names[i] = fmt.Sprintf("新人%c", '甲'+i)
			}
			claim, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "welcome-d", "topic": "welcome", "mission_kind": "welcome_named", "nicknames": names, "mission_event_count": n, "purpose_knowledge": knowledge, "expires_at": e.ExpiresAt}})
			core.claimRaw = string(claim)
			fixture := &welcomeCoreFixture{fakeCore: core, worker: w, tts: tts, knowledge: knowledge}
			w.core = fixture
			if err := w.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			if tts.calls != 1 || fixture.admissions != 1 || fixture.readyBeforeTTS || core.dispatches != 1 || agent.calls != 0 {
				t.Fatal("not one prepared welcome", tts.calls, fixture.admissions, fixture.readyBeforeTTS, core.dispatches, agent.calls)
			}
			if !strings.Contains(tts.last.Text, e.Body) {
				t.Fatal("welcome body changed", tts.last.Text)
			}
		})
	}
}

func TestWelcomeWorkerCutRetriesReuseAudioWithoutRenewingTTL(t *testing.T) {
	w, _, core, _, tts, session, e := v2Fixture(t, 50020)
	e.Body = "欢迎新来的朋友。"
	e.Intent.Question = false
	now := time.Now()
	w.now = func() time.Time { return now }
	raw, _ := json.Marshal(e)
	var knowledge map[string]any
	json.Unmarshal(raw, &knowledge)
	knowledge["welcome_quota"] = true
	setClaim := func() {
		claim, _ := json.Marshal(map[string]any{"claimed": true, "item": map[string]any{"id": "welcome-d", "topic": "welcome", "mission_kind": "welcome_named", "nicknames": []string{"小王"}, "purpose_knowledge": knowledge, "expires_at": knowledge["expires_at"]}})
		core.claimRaw = string(claim)
	}
	setClaim()
	core.dispatchStatus = 409
	core.dispatchRaw = `{"error":"当前35秒内没有安全句末切点，继续排队"}`
	fixture := &welcomeCoreFixture{fakeCore: core, worker: w, tts: tts, knowledge: knowledge}
	w.core = fixture
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	deadline := fixture.ready.Add(30 * time.Second)
	now = now.Add(5 * time.Second)
	setClaim()
	core.dispatchStatus = 0
	if err := w.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	if tts.calls != 1 || fixture.admissions != 2 || !fixture.ready.Add(30*time.Second).Equal(deadline) {
		t.Fatal("retry renewed or synthesized again", tts.calls, fixture.admissions)
	}
	now = deadline
	setClaim()
	before := core.dispatches
	if err := w.processRoom(context.Background(), session); err == nil || core.dispatches != before || tts.calls != 1 {
		t.Fatal("expired old audio replayed")
	}
}
