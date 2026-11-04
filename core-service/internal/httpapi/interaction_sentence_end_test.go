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

	"livecompanion/core/internal/agentdecision"
	"livecompanion/core/internal/agentwork"
	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/coreaudio"
	roomstore "livecompanion/core/internal/room"
)

func TestLegacyNonSequentialHTTPWithoutSentenceBoundaryLeavesMainlinePlaying(t *testing.T) {
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
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{RoomID: 94, Tracks: []audioout.ProgramTrack{{ID: "unverified", AudioURL: audio.URL, Text: "未完成的一句话，", DurationMS: 1000}}})
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
	if !strings.Contains(response.Body.String(), "没有真实PCM声学切点") {
		t.Fatal("missing boundary lost the management prepared-audio retry contract", response.Body.String())
	}
}
