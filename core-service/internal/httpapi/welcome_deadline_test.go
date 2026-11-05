package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
)

func TestClaimPollingPreservesWelcomePastAdmissionUntilActualReturn(t *testing.T) {
	s, old := v2ServerItem(t)
	identity, _ := decodeInteractionV2Item(*old)
	s.agentDecisions.ClearRoom(94)
	now := time.Now()
	deadline := now.Add(-426 * time.Millisecond)
	clock := deadline.Add(-30 * time.Second)
	s.agentDecisions = agentdecision.NewWithClock(func() time.Time { return clock }, time.Minute, time.Minute, 24)
	identity.Expires = deadline
	raw, _ := json.Marshal(identity)
	var envelope map[string]any
	json.Unmarshal(raw, &envelope)
	envelope["welcome_quota"] = true
	raw, _ = json.Marshal(envelope)
	item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "playing-welcome", MissionKind: "welcome_batch", PurposeKnowledge: raw, EventExpiresAt: deadline, TTLSeconds: 60}).Item
	s.agentDecisions.ClaimNext(94)
	envelope["welcome_ready_at"] = clock
	raw, _ = json.Marshal(envelope)
	if !s.agentDecisions.AdmitWelcome(94, item.ID, deadline, raw) {
		t.Fatal("welcome admission")
	}
	clock = clock.Add(13269 * time.Millisecond)
	if !s.agentDecisions.BindPlayback(94, item.ID, "started-pcm") {
		t.Fatal("bind")
	}
	clock = now
	for i := 0; i < 3; i++ {
		r := httptest.NewRequest(http.MethodPost, "/claim?tenant_id=7", nil)
		r.SetPathValue("roomID", "94")
		out := httptest.NewRecorder()
		s.claimRoomAgentDecision(out, r)
		if out.Code != 200 || !strings.Contains(out.Body.String(), `"reason":"deferred"`) {
			t.Fatal(out.Code, out.Body.String())
		}
		if _, ok := s.agentDecisions.Get(94, item.ID); !ok || len(s.agentDecisions.Snapshot(94).TerminalHistory) != 0 {
			t.Fatal("poll removed actual playback")
		}
	}
	clock = clock.Add(2 * time.Second)
	if _, ok := s.agentDecisions.Complete(94, item.ID); !ok {
		t.Fatal("actual return lost")
	}
	terminal := s.agentDecisions.Snapshot(94).TerminalHistory
	if len(terminal) != 1 || terminal[0].Status != "COMPLETED" || terminal[0].Reason != "actual_mainline_return" {
		t.Fatal(terminal)
	}
}

func TestWelcomeTTSPreflightUsesEarlierNameDeadlineAndPreparationEstimate(t *testing.T) {
	for _, tc := range []struct {
		name      string
		remaining time.Duration
		prep      int
		ready     bool
		stage     string
		want      string
	}{
		{"original_names_expired_lease_alive", -time.Second, 5000, false, "tts", "welcome_tts_time_insufficient"},
		{"historical_2_second_margin", 2315 * time.Millisecond, 5000, false, "tts", "welcome_tts_time_insufficient"},
		{"long_synthesis_needs_more_time", 10 * time.Second, 12000, false, "tts", "welcome_tts_time_insufficient"},
		{"missing_names_deadline", 0, 5000, false, "tts", "welcome_names_deadline_missing"},
		{"fresh_sufficient_time", 20 * time.Second, 5000, false, "tts", ""},
		{"ready_audio_can_wait_for_playback", -time.Second, 5000, true, "tts", ""},
		{"model_stage_keeps_separate_lease", 2 * time.Second, 5000, false, "model", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, old := v2ServerItem(t)
			identity, _ := decodeInteractionV2Item(*old)
			s.agentDecisions.ClearRoom(94)
			raw, _ := json.Marshal(identity)
			var envelope map[string]any
			json.Unmarshal(raw, &envelope)
			envelope["welcome_quota"] = true
			if tc.remaining != 0 {
				envelope["welcome_names_expires_at"] = time.Now().Add(tc.remaining)
			}
			if tc.ready {
				envelope["welcome_ready_at"] = time.Now()
			}
			raw, _ = json.Marshal(envelope)
			item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "welcome", Question: "欢迎", PurposeKnowledge: raw, TTLSeconds: 60}).Item
			s.agentDecisions.ClaimNext(94)
			s.agentWork.SetMode(94, agentwork.ModeControl)
			body, _ := json.Marshal(audioout.InteractionPreflightRequest{Stage: tc.stage, Text: "欢迎新来的朋友。", PreparationMS: tc.prep})
			r := httptest.NewRequest(http.MethodPost, "/preflight?tenant_id=7", bytes.NewReader(body))
			r.SetPathValue("roomID", "94")
			r.SetPathValue("decisionID", item.ID)
			out := httptest.NewRecorder()
			s.preflightRoomAgentDecision(out, r)
			var result audioout.InteractionPreflightResult
			json.Unmarshal(out.Body.Bytes(), &result)
			if out.Code != 200 || result.Reason != tc.want || result.Allowed != (tc.want == "") {
				t.Fatal(out.Code, out.Body.String())
			}
		})
	}
}
