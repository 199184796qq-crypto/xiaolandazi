package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audioout"
	"net/http/httptest"
	"testing"
	"time"
)

type preparationWindowClient struct {
	fakeAudioTaskClient
	cuts []audioout.AcousticPoint
}

func (c *preparationWindowClient) ProgramSnapshot(context.Context, int64) (audioout.RoomProgramSnapshot, error) {
	return audioout.RoomProgramSnapshot{Running: true, RoomID: 94, ProgramID: "run", TrackID: "main", CurrentMS: 1000, BufferedMainlineMS: 120000, AcousticReady: true, AcousticCuts: c.cuts, ActualStartedAt: time.Now().Add(-4 * time.Minute), Task: &audioout.SpeechTask{ID: "main", RoomID: 94, DurationMS: 120000}}, nil
}

func TestMissingPCMWindowYieldsBeforeClaimAndPreflight(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentWork.SetMode(94, agentwork.ModeAnchor)
	client := &preparationWindowClient{}
	s.SetAudioClient(client, "")
	req := httptest.NewRequest("POST", "/claim?tenant_id=7", nil)
	req.SetPathValue("roomID", "94")
	out := httptest.NewRecorder()
	s.claimRoomAgentDecision(out, req)
	var result map[string]any
	json.Unmarshal(out.Body.Bytes(), &result)
	if out.Code != 200 || result["claimed"] != false || result["reason"] != "no_pcm_cut" {
		t.Fatal(out.Code, out.Body.String())
	}
	current, _ := s.agentDecisions.Get(94, item.ID)
	if current.Attempts != 0 || current.ClaimedAt != nil || current.AudioPrepared || !current.ExpiresAt.Equal(item.ExpiresAt) {
		t.Fatal("preflight renewed or charged task", current)
	}
	client.cuts = []audioout.AcousticPoint{{Grade: "B", CutMS: 5000}}
	if got := s.InteractionPreparationWindow(context.Background(), audioout.InteractionPreflightRequest{RoomID: 94, Until: time.Now().Add(time.Minute)}); !got.Allowed || got.CutMS != 5000 {
		t.Fatal("real acoustic window rejected", got)
	}
	// Caller-provided original validity remains a hard bound.
	if got := s.InteractionPreparationWindow(context.Background(), audioout.InteractionPreflightRequest{RoomID: 94, Until: time.Now().Add(time.Second)}); got.Allowed {
		t.Fatal("cut beyond original deadline accepted")
	}
}
func TestTTSPreflightRequiresOwnedClaimAndPhysicalWindow(t *testing.T) {
	s, item := v2ServerItem(t)
	s.agentWork.SetMode(94, agentwork.ModeAnchor)
	s.SetAudioClient(&preparationWindowClient{}, "")
	s.agentDecisions.ClaimNext(94)
	req := httptest.NewRequest("POST", "/preflight?tenant_id=7", bytes.NewBufferString(`{"stage":"tts","duration_ms":16080}`))
	req.SetPathValue("roomID", "94")
	req.SetPathValue("decisionID", item.ID)
	out := httptest.NewRecorder()
	s.preflightRoomAgentDecision(out, req)
	var r audioout.InteractionPreflightResult
	json.Unmarshal(out.Body.Bytes(), &r)
	if out.Code != 200 || r.Allowed || r.Reason != "no_pcm_cut" {
		t.Fatal(out.Code, out.Body.String())
	}
}
