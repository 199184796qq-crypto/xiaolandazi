package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	roomstore "livecompanion/core/internal/room"
	"livecompanion/core/internal/roomaudio"
	"livecompanion/core/internal/speechruntime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type phase1RoomDriver struct{}
type phase1RoomConn struct{}
type phase1RoomRows struct{ done bool }

func (phase1RoomDriver) Open(string) (driver.Conn, error)  { return phase1RoomConn{}, nil }
func (phase1RoomConn) Prepare(string) (driver.Stmt, error) { return nil, fmt.Errorf("unused") }
func (phase1RoomConn) Close() error                        { return nil }
func (phase1RoomConn) Begin() (driver.Tx, error)           { return nil, fmt.Errorf("unused") }
func (phase1RoomConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	return &phase1RoomRows{}, nil
}
func (*phase1RoomRows) Columns() []string {
	return strings.Split("id,tenant,platform,external,source,name,status,collector,monitor,monitor_started,device,online,last_event,created,updated", ",")
}
func (*phase1RoomRows) Close() error { return nil }
func (r *phase1RoomRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	copy(values, []driver.Value{int64(94), int64(7), "douyin", "phase1-test", "", "isolated PCM", "live", "auto", false, nil, false, int64(0), nil, time.Now(), time.Now()})
	return nil
}
func init() { sql.Register("phase1-return-room", phase1RoomDriver{}) }
func phase1PCM(duration int) []byte {
	size := duration * roomaudio.SampleRate * 2 / 1000
	wav := make([]byte, 44+size)
	copy(wav, "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(36+size))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], roomaudio.SampleRate)
	binary.LittleEndian.PutUint32(wav[28:32], roomaudio.SampleRate*2)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(size))
	return wav
}

func TestInteractionHTTPBridgePCMAndReturnCompletion(t *testing.T) {
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
	s.SetAudioClient(client, "")
	_, _ = s.agentWork.SetMode(94, agentwork.ModeAnchor)
	_, _ = s.agentWork.StartAgent(94, 0)
	audio := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		duration := 3400
		switch r.URL.Path {
		case "/answer":
			duration = 1600
		case "/bridge":
			duration = 200
		case "/b":
			duration = 3000
		}
		w.Write(phase1PCM(duration))
	}))
	defer audio.Close()
	_, err = client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Sequential: true, Tracks: []audioout.ProgramTrack{{ID: "a", AudioURL: audio.URL + "/a", Text: "切入前完整语义。", DurationMS: 3400, Timeline: []audioout.ProgramTimelineSegment{{SegmentID: "a1", Index: 1, EndMS: 3400, Text: "切入前完整语义。", SafeCut: true}}, SafePoints: []audioout.ProgramSafePoint{{ID: "p1", SentenceID: "a1", Kind: "SENTENCE", Score: 90, CutMS: 3400, Grade: "A"}}}, {ID: "b", AudioURL: audio.URL + "/b", Text: "回归后的下一完整单元。", DurationMS: 3000}}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.StopProgram(context.Background(), 94)
	decision := s.agentDecisions.Enqueue(94, agentdecision.Candidate{Topic: "PURPOSE:price", Scene: "product_question", Question: "多少钱", MaxRetries: 2}).Item
	claimed, ok := s.agentDecisions.ClaimNext(94)
	if !ok {
		t.Fatal("claim")
	}
	frames, stop, err := s.roomAudio.Subscribe(94)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		raw, _ := json.Marshal(roomAudioInteractionInput{DecisionID: claimed.ID, Action: "answer", AudioURL: audio.URL + "/answer", Question: "多少钱", ReplyText: "这是完整的核心互动回答。", DeferredReturn: true, SwitchAtMS: 3400})
		r := httptest.NewRequest("POST", "/internal/v1/rooms/94/audio/interaction?tenant_id=7", bytes.NewReader(raw))
		r.Header.Set("X-Core-Token", "probe-token")
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, r)
		done <- rec
	}()
	var plan audioout.InteractionReturnSnapshot
	until := time.Now().Add(time.Second)
	for time.Now().Before(until) {
		plan, err = client.InteractionReturnSnapshot(context.Background(), 94, "")
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("no pre-cut plan", err)
	}
	raw, _ := json.Marshal(map[string]any{"decision_id": decision.ID, "task_id": plan.TaskID, "token": plan.Token, "text": "刚才说到用法，咱们接着讲。", "audio_url": audio.URL + "/bridge"})
	r := httptest.NewRequest("POST", "/internal/v1/rooms/94/audio/interaction-bridge?tenant_id=7", bytes.NewReader(raw))
	r.Header.Set("X-Core-Token", "probe-token")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatal("bridge stage", rec.Code, rec.Body.String())
	}
	dispatch := <-done
	if dispatch.Code != 200 {
		t.Fatal("dispatch", dispatch.Code, dispatch.Body.String())
	}
	s.applyAudioInteractionPlaybackEvent(context.Background(), s.audioDevState(), audioout.PlaybackEvent{RoomID: 94, SpeechTaskID: plan.TaskID, ReceiverID: "media-only", Status: "COMPLETED", OccurredAt: time.Now()})
	if speech, _ := s.speechRuntime.Snapshot(94); speech.Interrupt.Status == speechruntime.StatusCompleted {
		t.Fatal("early media-only terminal completed task")
	}
	until = time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		select {
		case <-frames:
		default:
		}
		if _, exists := s.agentDecisions.Get(94, decision.ID); !exists {
			snapshot := s.agentDecisions.Snapshot(94)
			if snapshot.Completion.Completed != 1 {
				t.Fatal("wrong completion accounting", snapshot.Completion)
			}
			speech, _ := s.speechRuntime.Snapshot(94)
			if speech.Interrupt.Status != speechruntime.StatusCompleted || !speech.Interrupt.BridgeUsed {
				t.Fatal("answer/bridge/return incomplete", speech.Interrupt)
			}
			srtRequest := httptest.NewRequest("GET", "/internal/v1/rooms/94/composite.srt?tenant_id=7", nil)
			srtRequest.Header.Set("X-Core-Token", "probe-token")
			srtResponse := httptest.NewRecorder()
			s.Handler().ServeHTTP(srtResponse, srtRequest)
			if srtResponse.Code != 200 || !strings.Contains(srtResponse.Body.String(), "刚才说到用法") || !strings.Contains(srtResponse.Body.String(), "回归后的下一完整单元") {
				t.Fatal("SRT export missing actual bridge/return", srtResponse.Code, srtResponse.Body.String())
			}
			unauthorized := httptest.NewRecorder()
			s.Handler().ServeHTTP(unauthorized, httptest.NewRequest("GET", "/internal/v1/rooms/94/composite.srt?tenant_id=7", nil))
			if unauthorized.Code != 401 {
				t.Fatal("subtitle export bypassed Core authentication", unauthorized.Code)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("selected event did not complete after actual resumed PCM")
}
