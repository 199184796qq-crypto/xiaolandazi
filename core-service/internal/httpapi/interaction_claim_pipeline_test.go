package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/messagecaptain"
	"livecompanion/core/internal/paidpipeline"
	"net/http/httptest"
	"testing"
)

func TestClaimHTTPWithActualPreflightThenModelAndTTS(t *testing.T) {
	for _, mission := range []string{"welcome_named", "welcome_batch", "reply_question", "warmup", "like_follow", "time_report"} {
		t.Run(mission, func(t *testing.T) { claimThroughActualPipeline(t, mission) })
	}
}

func claimThroughActualPipeline(t *testing.T, mission string) {
	t.Helper()
	s, item := v2ServerItem(t)
	s.agentDecisions.ClearRoom(94)
	item = s.agentDecisions.Enqueue(94, agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "body-task", Question: "哪里发货", PurposeKnowledge: item.PurposeKnowledge, TTLSeconds: 60, MissionKind: mission, Scene: "interaction_v2"}).Item
	s.agentWork.SetMode(94, agentwork.ModeAnchor)
	s.SetAudioClient(&preparationWindowClient{cuts: []audioout.AcousticPoint{{Grade: "B", CutMS: 5000}}}, "")
	p := paidpipeline.New(s.agentWork, s.agentDecisions)
	p.SetMessageCaptain(s.messages)
	seen := map[string]bool{}
	s.messages.Register("claim-contract-proof", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind != "interaction.v2.preflight" {
			return nil
		}
		var input audioout.InteractionPreflightRequest
		if err := json.Unmarshal(m.Payload, &input); err != nil {
			t.Fatal(err)
		}
		if input.Epoch != s.currentInteractionEpoch(94) || m.RunID != input.Epoch || input.PlanID != 3 || input.RoomID != 94 || input.TenantID != 7 || input.Until.IsZero() {
			t.Errorf("incomplete runtime fence: %+v run=%s", input, m.RunID)
		}
		current, _ := s.agentDecisions.Get(94, item.ID)
		want := agentdecision.StatusClaimed
		if input.Stage == "claim" {
			want = agentdecision.StatusPending
		}
		if current.Status != want {
			t.Errorf("stage %s status %s want %s", input.Stage, current.Status, want)
		}
		seen[input.Stage] = true
		return nil
	})
	r := httptest.NewRequest("POST", "/claim?tenant_id=7", nil)
	r.SetPathValue("roomID", "94")
	out := httptest.NewRecorder()
	s.claimRoomAgentDecision(out, r)
	var result struct {
		Claimed bool `json:"claimed"`
	}
	json.Unmarshal(out.Body.Bytes(), &result)
	if out.Code != 200 || !result.Claimed {
		t.Fatal("actual HTTP claim blocked", out.Code, out.Body.String())
	}
	for _, stage := range []string{"model", "tts"} {
		raw, _ := json.Marshal(map[string]any{"stage": stage, "duration_ms": 3000})
		r = httptest.NewRequest("POST", "/preflight?tenant_id=7", bytes.NewReader(raw))
		r.SetPathValue("roomID", "94")
		r.SetPathValue("decisionID", item.ID)
		out = httptest.NewRecorder()
		s.preflightRoomAgentDecision(out, r)
		var check audioout.InteractionPreflightResult
		json.Unmarshal(out.Body.Bytes(), &check)
		if out.Code != 200 || !check.Allowed {
			t.Fatalf("%s blocked after actual claim: %d %s", stage, out.Code, out.Body.String())
		}
	}
	for _, stage := range []string{"claim", "model", "tts"} {
		if !seen[stage] {
			t.Fatal("missing actual stage", stage)
		}
	}
}

func TestClaimHTTPDoesNotReviveTaskRemovedDuringPreflight(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentWork.SetMode(94, agentwork.ModeAnchor)
	s.SetAudioClient(&preparationWindowClient{cuts: []audioout.AcousticPoint{{Grade: "B", CutMS: 5000}}}, "")
	p := paidpipeline.New(s.agentWork, s.agentDecisions)
	p.SetMessageCaptain(s.messages)
	s.messages.Register("cancel-during-claim", func(_ context.Context, m messagecaptain.Message) error {
		if m.Kind == "interaction.v2.preflight" {
			s.agentDecisions.Remove(94, item.ID)
		}
		return nil
	})
	r := httptest.NewRequest("POST", "/claim?tenant_id=7", nil)
	r.SetPathValue("roomID", "94")
	out := httptest.NewRecorder()
	s.claimRoomAgentDecision(out, r)
	var result struct {
		Claimed bool `json:"claimed"`
	}
	json.Unmarshal(out.Body.Bytes(), &result)
	if out.Code != 200 || result.Claimed {
		t.Fatal(out.Code, out.Body.String())
	}
	if _, ok := s.agentDecisions.Get(94, item.ID); ok {
		t.Fatal("cancelled task revived")
	}
}
