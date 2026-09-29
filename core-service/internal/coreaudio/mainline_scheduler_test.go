package coreaudio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/core/internal/audioout"
	"livecompanion/core/internal/roomaudio"
)

func TestMainlineAgentChoosesContextRelevantTrack(t *testing.T) {
	client, _ := newTestClient(t, 100)
	now := time.Now().UTC()
	client.now = func() time.Time { return now }
	client.SetMainlineAgentSignalProvider(func(int64) MainlineAgentSignals {
		return MainlineAgentSignals{
			Heat:             "WARM",
			QuestionPressure: 0.8,
			TopTopics:        []string{"FAMILY:发货物流"},
		}
	})
	program := &programState{
		RoomID:     99,
		Sequence:   7,
		TrackIndex: 0,
		Tracks: []programTrack{
			{ID: "A", Text: "讲品牌故事和传统工艺。", DurationMS: 50000},
			{ID: "B", Text: "今天价格优惠，去链接下单。", DurationMS: 50000},
			{ID: "C", Text: "中通极兔邮政随机发货，包邮送到家。", DurationMS: 50000},
			{ID: "D", Text: "讲原料和菜籽油香味。", DurationMS: 50000},
		},
		Running:         true,
		TrackPlayCount:  map[string]int{"A": 1},
		TrackLastPlayed: map[string]time.Time{"A": now},
		RecentTrackIDs:  []string{"A"},
	}
	client.mu.Lock()
	client.programs[99] = program
	client.mu.Unlock()

	next, _, _ := client.selectNextMainlineTrack(program, 0)
	if next != 2 {
		t.Fatalf("shipping pressure should select C, got index=%d id=%s", next, program.Tracks[next].ID)
	}
}

func TestRoomAudioMainlineAdvancesOnActualPCMEOF(t *testing.T) {
	client, _ := newTestClient(t, 100)
	engine := roomaudio.New()
	client.SetRoomAudioEngine(engine)

	makeDelayedServer := func(durationMS int, delayMirror time.Duration) *httptest.Server {
		audio := testWAV(durationMS)
		var requests atomic.Int32
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			requestNo := requests.Add(1)
			if requestNo >= 2 && delayMirror > 0 {
				time.Sleep(delayMirror)
			}
			w.Header().Set("Content-Type", "audio/wav")
			_, _ = w.Write(audio)
		}))
	}

	serverA := makeDelayedServer(80, 100*time.Millisecond)
	defer serverA.Close()
	serverB := makeDelayedServer(80, 0)
	defer serverB.Close()

	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{
		RoomID:    77,
		SessionID: "s77",
		Tracks: []audioout.ProgramTrack{
			{ID: "A", Label: "A稿", AudioURL: serverA.URL},
			{ID: "B", Label: "B稿", AudioURL: serverB.URL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if start.TrackID != "A" {
		t.Fatalf("expected A at start: %#v", start)
	}

	time.Sleep(95 * time.Millisecond)
	mid, err := client.ProgramSnapshot(context.Background(), 77)
	if err != nil {
		t.Fatal(err)
	}
	if mid.TrackID != "A" {
		t.Fatalf("mainline advanced before actual PCM EOF: %#v", mid)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		next, nextErr := client.ProgramSnapshot(context.Background(), 77)
		if nextErr == nil && next.TrackID == "B" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("mainline did not advance to B after actual A PCM EOF")
}

func TestMainlineResumeDoesNotCountAsNewTrackSelection(t *testing.T) {
	client, _ := newTestClient(t, 200)
	path := filepath.Join(t.TempDir(), "resume.wav")
	if err := os.WriteFile(path, testWAV(200), 0o600); err != nil {
		t.Fatal(err)
	}
	server := newWAVServer(t, path)
	defer server.Close()

	client.SetRoomAudioEngine(roomaudio.New())
	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{
		RoomID: 88,
		Tracks: []audioout.ProgramTrack{
			{ID: "A", AudioURL: server.URL},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	client.mu.RLock()
	program := client.programs[88]
	initialCount := program.TrackPlayCount["A"]
	client.mu.RUnlock()
	if initialCount != 1 {
		t.Fatalf("expected one initial selection, got %d", initialCount)
	}

	client.cancelRoomAudioMirror(88)
	client.hub.Expire(start.Task.ID)
	if _, err := client.publishMainline(program, start.Sequence+1, 60, time.Now().Add(-60*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	client.mu.RLock()
	resumeCount := program.TrackPlayCount["A"]
	client.mu.RUnlock()
	if resumeCount != 1 {
		t.Fatalf("resume must not count as a new agent selection, got %d", resumeCount)
	}
}
