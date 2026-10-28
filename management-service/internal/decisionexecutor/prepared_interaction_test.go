package decisionexecutor

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
)

type preparedTestStore struct {
	*fakeStore
	memories []model.AgentMemoryItem
	history  []model.GeneratedSpeechHistoryInput
}

func (s *preparedTestStore) ListActiveAgentMemories(context.Context, int64, int64) ([]model.AgentMemoryItem, error) {
	return s.memories, nil
}

func (s *preparedTestStore) RecordGeneratedSpeechHistory(_ context.Context, input model.GeneratedSpeechHistoryInput) error {
	s.history = append(s.history, input)
	return nil
}

func preparedTestWorker() (*Worker, *preparedTestStore, *fakeCore, *fakeAgent, *fakeTTS, model.LiveRuntimeSession) {
	store := &preparedTestStore{fakeStore: readyVoiceStore()}
	core := &fakeCore{dispatchStatus: http.StatusConflict, dispatchRaw: `{"error":"主线已换段，请重新选择切点"}`}
	agent, tts := &fakeAgent{}, &fakeTTS{}
	worker := New(store, core, agent, tts)
	return worker, store, core, agent, tts, model.LiveRuntimeSession{ID: 9, TenantID: 7, RoomID: 11, Status: "running"}
}

func TestPreparedInteractionReclaimDoesNotRegenerateTextOrVoice(t *testing.T) {
	worker, store, core, agent, tts, session := preparedTestWorker()
	ctx := context.Background()
	if err := worker.processRoom(ctx, session); err != nil {
		t.Fatal(err)
	}
	if len(store.history) != 0 || core.releases != 1 || len(worker.prepared) != 1 {
		t.Fatal("rejected audio was treated as delivered or was lost")
	}
	core.dispatchStatus = 0
	core.claimRaw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","title":"发货物流","sample_questions":["什么时候发货"],"manual_action":"answer"},"switch_at_ms":7777,"current_mainline":"另一段主线","resume_mainline":"下一句","resume_segment_id":"new-segment"}`
	if err := worker.processRoom(ctx, session); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || tts.calls != 1 || core.dispatches != 4 {
		t.Fatalf("cut reschedule repeated paid generation: model=%d tts=%d dispatch=%d", agent.calls, tts.calls, core.dispatches)
	}
	if core.dispatch["switch_at_ms"] != 0 || core.dispatch["audio_url"] != "http://audio.local/reply.wav" {
		t.Fatal("reschedule reused the old cut or changed the prepared audio")
	}
	if len(worker.prepared) != 0 || len(store.history) != 1 || core.releases != 1 {
		t.Fatal("successful submission was cached/released again or not recorded once")
	}
}

func TestPreparedInteractionInvalidatesChangedFactsCorrectionsVoiceAndQuestion(t *testing.T) {
	cases := []struct {
		name   string
		change func(*preparedTestStore, *fakeCore)
	}{
		{"facts", func(s *preparedTestStore, _ *fakeCore) {
			s.facts = []model.LiveAgentPlanFact{{Key: "shipping", Value: "改为周五发出", Status: "active"}}
		}},
		{"correction", func(s *preparedTestStore, _ *fakeCore) {
			s.memories = []model.AgentMemoryItem{{ID: 1, TenantID: 7, RoomID: 11, Status: "active", MemoryType: "style", MemoryKey: "short", CurrentVersion: &model.AgentMemoryVersion{ContentText: "回答更简短", Structured: map[string]any{"review_plan_id": s.plan.ID}}}}
		}},
		{"voice", func(s *preparedTestStore, _ *fakeCore) { s.publishedVersion.VoiceIdentity.VoiceID = "new-voice" }},
		{"question", func(_ *preparedTestStore, core *fakeCore) {
			core.claimRaw = `{"claimed":true,"item":{"id":"d-1","topic":"FAMILY:发货物流","sample_questions":["发什么快递"],"manual_action":"answer"}}`
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			worker, store, core, agent, tts, session := preparedTestWorker()
			if err := worker.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			tc.change(store, core)
			core.dispatchStatus = 0
			if err := worker.processRoom(context.Background(), session); err != nil {
				t.Fatal(err)
			}
			if agent.calls < 2 || tts.calls != 2 {
				t.Fatalf("changed %s reused stale audio: model=%d tts=%d", tc.name, agent.calls, tts.calls)
			}
		})
	}
}

func TestPreparedInteractionLifetimeIsNotExtendedByCutRetries(t *testing.T) {
	worker, _, _, _, _, session := preparedTestWorker()
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	worker.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		if err := worker.processRoom(context.Background(), session); err != nil {
			t.Fatal(err)
		}
		for _, pending := range worker.prepared {
			if !pending.ExpiresAt.Equal(time.Date(2026, 10, 9, 0, 2, 0, 0, time.UTC)) {
				t.Fatal("repeated cut rejection extended the prepared lifetime")
			}
		}
		now = now.Add(time.Minute)
	}
	worker.prunePreparedInteractions([]model.LiveRuntimeSession{session})
	if len(worker.prepared) != 0 {
		t.Fatal("expired prepared audio survived pruning")
	}
}

func TestPreparedInteractionIsIsolatedBySessionAndClearedOnPause(t *testing.T) {
	worker, _, core, agent, tts, session := preparedTestWorker()
	if err := worker.processRoom(context.Background(), session); err != nil {
		t.Fatal(err)
	}
	newSession := session
	newSession.ID++
	core.dispatchStatus = 0
	if err := worker.processRoom(context.Background(), newSession); err != nil {
		t.Fatal(err)
	}
	if agent.calls != 2 || tts.calls != 2 {
		t.Fatal("new session reused an old session's prepared interaction")
	}
	session.Status = "paused"
	worker.prunePreparedInteractions([]model.LiveRuntimeSession{session})
	if len(worker.prepared) != 0 {
		t.Fatal("paused/stopped session kept prepared execution state")
	}
}

type transportErrorCore struct{ *fakeCore }

func (c transportErrorCore) DoRoom(ctx context.Context, tenant, room int64, method, path string, query url.Values, body any) (*http.Response, error) {
	if strings.HasSuffix(path, "/audio/interaction") {
		c.dispatches++
		return nil, errors.New("connection closed after submission")
	}
	return c.fakeCore.DoRoom(ctx, tenant, room, method, path, query, body)
}

func TestPreparedInteractionNeverRetainsOrRepeatsUncertainSubmission(t *testing.T) {
	worker, _, core, agent, tts, session := preparedTestWorker()
	worker.core = transportErrorCore{core}
	if err := worker.processRoom(context.Background(), session); err == nil {
		t.Fatal("expected uncertain transport failure")
	}
	if core.dispatches != 1 || agent.calls != 1 || tts.calls != 1 || len(worker.prepared) != 0 {
		t.Fatal("uncertain submission was repeated or retained for automatic reuse")
	}
}
