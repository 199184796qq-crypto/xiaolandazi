package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	roomstore "livecompanion/core/internal/room"
	"livecompanion/core/internal/roomaudio"
)

func TestQuickHTTPWaitsForActualFinalSentencePCM(t *testing.T) {
	for _, test := range []struct {
		name, text, action, strategy string
		duration                     int
	}{
		{"original_3400ms_replay", "开袋后可以直接食用。", "quick", "repeat_confirm", 3400},
		{"short_cup_tail", "杯盖需要按说明扣紧。", "quick", "repeat_confirm", 620},
		{"service_hard_cut_strategy", "预约后由客服确认时间。", "answer", "hard_cut", 920},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CORE_DEV_INTERACTION_LOG", t.TempDir()+"/interactions.jsonl")
			database, err := sql.Open("phase1-return-room", "")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			s := New(roomstore.NewStore(database), nil, nil, nil, nil, "development", "probe-token")
			hub := audiohub.New()
			s.SetAudioHub(hub)
			client, _ := coreaudio.New(hub, "http://core.local", "")
			client.SetRoomAudioEngine(s.roomAudio)
			s.SetAudioClient(client, "")
			_, _ = s.agentWork.SetMode(94, agentwork.ModeAnchor)
			_, _ = s.agentWork.StartAgent(94, 0)
			audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				duration := test.duration
				if r.URL.Path == "/answer" {
					duration = 1000
				}
				w.Write(phase1PCM(duration))
			}))
			defer audio.Close()
			frames, unsubscribe, err := s.roomAudio.Subscribe(94)
			if err != nil {
				t.Fatal(err)
			}
			defer unsubscribe()
			_, err = client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: true, Tracks: []audioout.ProgramTrack{
				{ID: "last-sentence", AudioURL: audio.URL + "/a", Text: test.text, DurationMS: test.duration, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "end", Index: 1, EndMS: test.duration, Text: test.text, SafeCut: true}}},
				{ID: "next", AudioURL: audio.URL + "/b", Text: "下一完整单元。", DurationMS: test.duration},
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer client.StopProgram(context.Background(), 94)
			until := time.Now().Add(time.Second)
			for s.roomAudio.Snapshot(94).MainlineCursorMS == 0 && time.Now().Before(until) {
				time.Sleep(time.Millisecond)
			}
			item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Topic: "sentence-end", Question: "测试问题", ManualAction: test.action}).Item
			if item == nil {
				t.Fatal("no decision")
			}
			if _, ok := s.agentDecisions.ClaimNext(94); !ok {
				t.Fatal("claim failed")
			}
			raw, _ := json.Marshal(roomAudioInteractionInput{DecisionID: item.ID, Action: test.action, InterruptStrategy: test.strategy, DeferredReturn: true, AudioURL: audio.URL + "/answer", Question: "测试问题", ReplyText: "这是互动回答。"})
			req := httptest.NewRequest("POST", "/internal/v1/rooms/94/audio/interaction?tenant_id=7", bytes.NewReader(raw))
			req.Header.Set("X-Core-Token", "probe-token")
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { s.Handler().ServeHTTP(response, req); close(done) }()
			mainlineFrames := 0
			deadline := time.After(6 * time.Second)
		waiting:
			for {
				select {
				case frame := <-frames:
					if frame.Source == roomaudio.SourceMainline {
						mainlineFrames++
					}
				case <-done:
					break waiting
				case <-deadline:
					t.Fatal("sentence-end dispatch did not finish")
				}
			}
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			var receipt struct {
				SwitchAtMS *int `json:"switch_at_ms"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.SwitchAtMS == nil || *receipt.SwitchAtMS != test.duration || s.roomAudio.Snapshot(94).MainlineCursorMS < test.duration {
				t.Fatal("cut before actual sentence end", receipt, s.roomAudio.Snapshot(94))
			}
			for len(frames) > 0 {
				if frame := <-frames; frame.Source == roomaudio.SourceMainline {
					mainlineFrames++
				}
			}
			wantFrames := (test.duration + roomaudio.FrameDurationMS - 1) / roomaudio.FrameDurationMS
			if mainlineFrames != wantFrames {
				t.Fatalf("last sentence lost PCM: frames=%d want=%d", mainlineFrames, wantFrames)
			}
			t.Logf("complete sentence emitted: %d/%d frames; switch=%dms", mainlineFrames, wantFrames, *receipt.SwitchAtMS)
		})
	}
}

func TestQuickHTTPWithoutSentenceBoundaryLeavesMainlinePlaying(t *testing.T) {
	database, _ := sql.Open("phase1-return-room", "")
	defer database.Close()
	s := New(roomstore.NewStore(database), nil, nil, nil, nil, "development", "probe-token")
	hub := audiohub.New()
	s.SetAudioHub(hub)
	client, _ := coreaudio.New(hub, "http://core.local", "")
	client.SetRoomAudioEngine(s.roomAudio)
	s.SetAudioClient(client, "")
	_, _ = s.agentWork.SetMode(94, agentwork.ModeAnchor)
	_, _ = s.agentWork.StartAgent(94, 0)
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(phase1PCM(1000)) }))
	defer audio.Close()
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "unverified", AudioURL: audio.URL, Text: "未完成的一句话，", DurationMS: 1000}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 94)
	item := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Topic: "no-boundary", Question: "测试", ManualAction: "quick"}).Item
	if _, ok := s.agentDecisions.ClaimNext(94); !ok {
		t.Fatal("claim failed")
	}
	raw, _ := json.Marshal(roomAudioInteractionInput{DecisionID: item.ID, Action: "quick", AudioURL: audio.URL, ReplyText: "测试回答。"})
	req := httptest.NewRequest("POST", "/internal/v1/rooms/94/audio/interaction?tenant_id=7", bytes.NewReader(raw))
	req.Header.Set("X-Core-Token", "probe-token")
	response := httptest.NewRecorder()
	s.Handler().ServeHTTP(response, req)
	current, _ := client.ProgramSnapshot(context.Background(), 94)
	if response.Code != 409 || current.Suspended || current.Task.ID != start.Task.ID {
		t.Fatal("missing boundary interrupted mainline", response.Code, current)
	}
	if !strings.Contains(response.Body.String(), "当前35秒内没有安全句末切点") {
		t.Fatal("missing boundary lost the management prepared-audio retry contract", response.Body.String())
	}
}
