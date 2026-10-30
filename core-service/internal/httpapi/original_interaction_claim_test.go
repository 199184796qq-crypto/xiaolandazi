package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	roomstore "livecompanion/core/internal/room"
)

func TestOriginalRecordingOrdinaryInteractionClaim(t *testing.T) {
	for _, tt := range []struct {
		name       string
		duration   int
		prepared   bool
		sequential bool
		wantClaim  bool
	}{
		{"recording_generates_answer", 60000, false, false, true},
		{"recording_prepared_answer", 60000, true, false, true},
		{"recording_near_end_protected", 8000, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			database, err := sql.Open("phase1-return-room", "")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			s := New(roomstore.NewStore(database), nil, nil, nil, nil, "development", "probe-token")
			hub := audiohub.New()
			s.SetAudioHub(hub)
			client, err := coreaudio.New(hub, "http://core.local", "")
			if err != nil {
				t.Fatal(err)
			}
			client.SetRoomAudioEngine(s.roomAudio)
			s.SetAudioClient(client, "")
			_, _ = s.agentWork.SetMode(94, agentwork.ModeAnchor)
			_, _ = s.agentWork.StartAgent(94, 0)
			wav := phase1PCM(tt.duration)
			audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(wav) }))
			defer audio.Close()
			cut := tt.duration / 2
			_, err = client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: tt.sequential, Tracks: []audioout.ProgramTrack{{
				ID: "recorded-mainline", AudioURL: audio.URL, DurationMS: tt.duration, Text: "规格按商品页选择。客服可以协助确认。",
				Timeline:   []audioout.ProgramTimelineSegment{{SegmentID: "first", Index: 1, EndMS: cut, Text: "规格按商品页选择。", SafeCut: true}, {SegmentID: "next", Index: 2, StartMS: cut, EndMS: tt.duration, Text: "客服可以协助确认。", SafeCut: true}},
				SafePoints: []audioout.ProgramSafePoint{{ID: "p1", SentenceID: "first", Kind: "SENTENCE", Grade: "A", CutMS: cut}},
			}}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 94)
			first := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Topic: "PURPOSE:spec", Question: "怎么选择规格", ManualAction: "answer", InteractionDecision: agentdecision.InteractionDecision{UniqueChatters30s: 3}}).Item
			if first == nil {
				t.Fatal("missing first decision")
			}
			if tt.prepared {
				if _, ok := s.agentDecisions.ClaimNext(94); !ok {
					t.Fatal("cannot prepare first decision")
				}
				if _, ok := s.agentDecisions.RescheduleCut(94, first.ID); !ok {
					t.Fatal("cannot release prepared answer to sentence-end scheduling")
				}
				time.Sleep(1100 * time.Millisecond)
			}
			second := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Topic: "PURPOSE:service", Question: "能咨询客服吗", ManualAction: "answer", InteractionDecision: agentdecision.InteractionDecision{UniqueChatters30s: 1}}).Item
			if second == nil {
				t.Fatal("missing second decision")
			}
			request := httptest.NewRequest("POST", "/internal/v1/rooms/94/agent-decisions/claim?tenant_id=7", nil)
			request.Header.Set("X-Core-Token", "probe-token")
			response := httptest.NewRecorder()
			s.Handler().ServeHTTP(response, request)
			if response.Code != 200 {
				t.Fatalf("claim HTTP %d: %s", response.Code, response.Body.String())
			}
			var result struct {
				Claimed bool                `json:"claimed"`
				Reason  string              `json:"reason"`
				Item    *agentdecision.Item `json:"item"`
				CutMS   int                 `json:"switch_at_ms"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Claimed != tt.wantClaim || result.Item == nil || result.Item.ID != first.ID {
				t.Fatalf("ordinary queue head claim: %s", response.Body.String())
			}
			if tt.wantClaim && (result.CutMS != cut || result.Item.ManualAction == "quick") {
				t.Fatalf("lost normal sentence-end scheduling: %s", response.Body.String())
			}
			if !tt.wantClaim && result.Reason != "mainline_low_buffer" {
				t.Fatalf("near-end protection changed: %s", response.Body.String())
			}
		})
	}
}
