package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
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

func TestAcousticSelectionUsesAudioGradeAndOriginalDeadline(t *testing.T) {
	points := []audioout.AcousticPoint{{CutMS: 1000, Grade: "B"}, {CutMS: 1600, Grade: "A"}, {CutMS: 40000, Grade: "A"}}
	for _, tc := range []struct{ now, limit, want int }{{0, 30000, 1600}, {0, 1200, 1000}, {1500, 30000, 0}, {200, 700, 0}, {0, 0, 0}} {
		got, ok := chooseAcousticInteractionCut(points, tc.now, tc.limit)
		if got != tc.want || ok != (tc.want > 0) {
			t.Fatalf("%+v => %d,%t", tc, got, ok)
		}
	}
}

func TestLiveHTTPUsesSameAcousticCutForBothProvidersAndMissionTypes(t *testing.T) {
	for _, tc := range []struct {
		name, action, mission string
		untimed               bool
		noPause               bool
	}{{"ali_welcome", "quick", "welcome_batch", true, false}, {"volcengine_question", "answer", "reply_question", false, false}, {"no_pause_despite_caption", "answer", "reply_question", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			db, _ := sql.Open("phase1-return-room", "")
			defer db.Close()
			s := New(roomstore.NewStore(db), nil, nil, nil, nil, "development", "probe-token")
			hub := audiohub.New()
			s.SetAudioHub(hub)
			client, _ := coreaudio.New(hub, "http://core.local", "")
			client.SetRoomAudioEngine(s.roomAudio)
			s.SetAudioClient(client, "")
			_, _ = s.agentWork.SetMode(94, agentwork.ModeAnchor)
			_, _ = s.agentWork.StartAgent(94, 0)
			wav := phase1PCM(2000)
			for i := 44; i < len(wav); i += 2 {
				ms := (i - 44) / 48
				v := uint16(5000)
				if !tc.noPause && ms >= 700 && ms < 900 {
					v = 0
				}
				binary.LittleEndian.PutUint16(wav[i:], v)
			}
			audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/answer" {
					w.Write(phase1PCM(200))
					return
				}
				w.Write(wav)
			}))
			defer audio.Close()
			_, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: tc.name, AudioURL: audio.URL, DurationMS: 2000, Text: "标点在结束处。", UntimedTranscript: tc.untimed, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "caption", Index: 1, EndMS: 2000, SafeCut: true, Text: "标点在结束处。"}}}}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 94)
			until := time.Now().Add(time.Second)
			for {
				p, _ := client.ProgramSnapshot(context.Background(), 94)
				if p.AcousticReady {
					break
				}
				if time.Now().After(until) {
					t.Fatal("no acoustic table")
				}
				time.Sleep(time.Millisecond)
			}
			item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Topic: tc.name, Question: "问题", ManualAction: tc.action, MissionKind: tc.mission}).Item
			if _, ok := s.agentDecisions.ClaimNext(94); !ok {
				t.Fatal("claim failed")
			}
			raw, _ := json.Marshal(roomAudioInteractionInput{DecisionID: item.ID, Action: tc.action, DeferredReturn: true, ResumeStrategy: "FUSION_SKIP", SkipCurrentSentence: true, AudioURL: audio.URL + "/answer", ReplyText: "回答。"})
			req := httptest.NewRequest("POST", "/internal/v1/rooms/94/audio/interaction?tenant_id=7", bytes.NewReader(raw))
			req.Header.Set("X-Core-Token", "probe-token")
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if tc.noPause {
				if rec.Code != 409 {
					t.Fatal("caption authorized a voiced cut", rec.Code, rec.Body.String())
				}
				return
			}
			if rec.Code != 200 {
				t.Fatal(rec.Code, rec.Body.String())
			}
			var receipt struct {
				SwitchAtMS *int `json:"switch_at_ms"`
				ResumeToMS int  `json:"resume_to_ms"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			p, _ := client.ProgramSnapshot(context.Background(), 94)
			if receipt.SwitchAtMS == nil || *receipt.SwitchAtMS != 800 || p.ResumeOffsetMS != 800 {
				t.Fatal("did not use PCM cut and exact resume", rec.Body.String(), p.ResumeOffsetMS)
			}
		})
	}
}
