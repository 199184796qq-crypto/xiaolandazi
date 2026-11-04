package httpapi

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	roomstore "livecompanion/core/internal/room"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Only the elapsed opening-observation clock is fixed; all claim/cut logic is unmodified.
type pastOpeningAcousticClient struct{ *coreaudio.Client }

func (c pastOpeningAcousticClient) ProgramSnapshot(ctx context.Context, room int64) (audioout.RoomProgramSnapshot, error) {
	p, err := c.Client.ProgramSnapshot(ctx, room)
	p.ActualStartedAt = time.Now().Add(-3 * time.Minute)
	return p, err
}
func TestReportClaimNeverWaitsForLegacyTextTimeline(t *testing.T) {
	var cases []struct {
		name                        string
		chatters                    int
		v2, timed, want, sequential bool
	}
	for _, seq := range []bool{false, true} {
		for _, chatters := range []int{0, 1, 2, 3, 4, 10} {
			cases = append(cases, struct {
				name                        string
				chatters                    int
				v2, timed, want, sequential bool
			}{fmt.Sprintf("seq_%t_chatters_%d", seq, chatters), chatters, false, false, true, seq})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := sql.Open("phase1-return-room", "")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := New(roomstore.NewStore(db), nil, nil, nil, nil, "development", "probe-token")
			hub := audiohub.New()
			s.SetAudioHub(hub)
			client, err := coreaudio.New(hub, "http://core.local", "")
			if err != nil {
				t.Fatal(err)
			}
			client.SetRoomAudioEngine(s.roomAudio)
			s.SetAudioClient(pastOpeningAcousticClient{client}, "")
			s.agentWork.SetPlan(94, 3, "plan")
			s.agentWork.SetMode(94, agentwork.ModeAnchor)
			s.agentWork.StartAgent(94, 0)
			wav := phase1PCM(40000)
			for i := 44; i+1 < len(wav); i += 2 {
				ms := (i - 44) / 48
				v := uint16(5000)
				if ms >= 6000 && ms < 6400 || ms >= 28000 && ms < 28400 {
					v = 0
				}
				binary.LittleEndian.PutUint16(wav[i:], v)
			}
			audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(wav) }))
			defer audio.Close()
			track := audioout.ProgramTrack{ID: "saved-pattern", AudioURL: audio.URL, DurationMS: 40000, Text: "完整主线正文。还有下一句。", UntimedTranscript: !tc.timed}
			if tc.timed {
				track.Timeline = []audioout.ProgramTimelineSegment{{SegmentID: "a", Index: 1, EndMS: 28000, Text: "完整主线正文。", SafeCut: true}, {SegmentID: "b", Index: 2, StartMS: 28000, EndMS: 40000, Text: "还有下一句。", SafeCut: true}}
				track.SafePoints = []audioout.ProgramSafePoint{{ID: "p1", SentenceID: "a", Kind: "SENTENCE", CutMS: 28000, Grade: "B", Score: 90}}
			}
			_, err = client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: tc.sequential, Tracks: []audioout.ProgramTrack{track}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 94)
			until := time.Now().Add(3 * time.Second)
			var program audioout.RoomProgramSnapshot
			for {
				program, err = client.ProgramSnapshot(context.Background(), 94)
				if err == nil && program.AcousticReady {
					break
				}
				if time.Now().After(until) {
					t.Fatal("local acoustic analysis not ready")
				}
				time.Sleep(time.Millisecond)
			}
			if len(program.AcousticCuts) == 0 {
				t.Fatal("fixture has no real acoustic cuts")
			}
			candidate := agentdecision.Candidate{Source: agentdecision.SourceAgent, Topic: "time-report", Question: "自然提一句时间", MissionKind: "time_report", TTLSeconds: 1200, Priority: 100, InteractionDecision: agentdecision.InteractionDecision{Handle: true, UniqueChatters30s: tc.chatters}}
			if tc.v2 {
				run := s.agentWork.Get(94)
				raw, _ := json.Marshal(interactionV2Envelope{Protocol: "interaction_v2", Task: "body-ready", Epoch: fmt.Sprintf("%s:%d:%d", run.BootID, run.PlanID, run.RunGeneration), Tenant: 7, Room: 94, Plan: 3, Expires: time.Now().Add(time.Minute)})
				candidate.PurposeKnowledge = raw
				candidate.MissionKind = "welcome_batch"
			}
			item := s.agentDecisions.Enqueue(94, candidate).Item
			req := httptest.NewRequest("POST", "/internal/v1/rooms/94/agent-decisions/claim?tenant_id=7", nil)
			req.Header.Set("X-Core-Token", "probe-token")
			out := httptest.NewRecorder()
			s.Handler().ServeHTTP(out, req)
			var got struct {
				Claimed bool   `json:"claimed"`
				Reason  string `json:"reason"`
			}
			if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &got) != nil {
				t.Fatal(out.Code, out.Body.String())
			}
			if got.Claimed != tc.want {
				t.Fatalf("want=%v got=%s", tc.want, out.Body.String())
			}

			t.Logf("REGRESSION chatter=%d v2=%t timed=%t acousticCuts=%d buffer=%d claimed=%t reason=%s ttl_seconds=%.0f decision=%s", tc.chatters, tc.v2, tc.timed, len(program.AcousticCuts), program.BufferedMainlineMS, got.Claimed, got.Reason, item.ExpiresAt.Sub(item.CreatedAt).Seconds(), item.ID)
		})
	}
}

func TestLegacySemanticReturnEndpointsArePermanentlyGone(t *testing.T) {
	s := New(nil, nil, nil, nil, nil, "development", "probe-token")
	for _, handler := range []func(http.ResponseWriter, *http.Request){s.getInteractionReturn, s.stageInteractionBridge} {
		req := httptest.NewRequest("POST", "/legacy", nil)
		out := httptest.NewRecorder()
		handler(out, req)
		if out.Code != http.StatusGone {
			t.Fatal(out.Code, out.Body.String())
		}
	}
}
