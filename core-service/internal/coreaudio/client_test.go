package coreaudio

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"livecompanion/core/internal/audiohub"
	"livecompanion/core/internal/audioout"
)

func testWAV(durationMS int) []byte {
	const sampleRate = 24000
	const bytesPerSample = 2
	samples := sampleRate * durationMS / 1000
	dataSize := samples * bytesPerSample
	audio := make([]byte, 44+dataSize)
	copy(audio[0:4], "RIFF")
	binary.LittleEndian.PutUint32(audio[4:8], uint32(len(audio)-8))
	copy(audio[8:12], "WAVE")
	copy(audio[12:16], "fmt ")
	binary.LittleEndian.PutUint32(audio[16:20], 16)
	binary.LittleEndian.PutUint16(audio[20:22], 1)
	binary.LittleEndian.PutUint16(audio[22:24], 1)
	binary.LittleEndian.PutUint32(audio[24:28], sampleRate)
	binary.LittleEndian.PutUint32(audio[28:32], sampleRate*bytesPerSample)
	binary.LittleEndian.PutUint16(audio[32:34], bytesPerSample)
	binary.LittleEndian.PutUint16(audio[34:36], 16)
	copy(audio[36:40], "data")
	binary.LittleEndian.PutUint32(audio[40:44], uint32(dataSize))
	return audio
}

func newTestClient(t *testing.T, durationMS int) (*Client, *audiohub.Hub) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mainline.wav")
	if err := os.WriteFile(path, testWAV(durationMS), 0o600); err != nil {
		t.Fatal(err)
	}
	hub := audiohub.New()
	client, err := New(hub, "http://core.local", path)
	if err != nil {
		t.Fatal(err)
	}
	return client, hub
}

func newWAVServer(t *testing.T, path string) *httptest.Server {
	t.Helper()
	audio, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(audio)
	}))
}

func TestProgramLoopsInsideCoreHub(t *testing.T) {
	client, hub := newTestClient(t, 80)
	snapshot, err := client.StartTestProgram(context.Background(), 12, "s12", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Running || snapshot.Task == nil || snapshot.Task.Kind != "test_wav_program" {
		t.Fatalf("unexpected start snapshot: %#v", snapshot)
	}
	firstID := snapshot.Task.ID
	time.Sleep(120 * time.Millisecond)
	snapshot, err = client.ProgramSnapshot(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task == nil || snapshot.Task.ID == firstID || snapshot.Sequence < 2 {
		t.Fatalf("mainline did not loop in Core: %#v", snapshot)
	}
	if active := hub.ActiveTask(12); active == nil || active.ID != snapshot.Task.ID {
		t.Fatalf("hub active task mismatch: %#v", active)
	}
}

func TestInteractionSuspendsAndResumesMainline(t *testing.T) {
	client, hub := newTestClient(t, 220)
	interactionPath := filepath.Join(t.TempDir(), "interaction.wav")
	if err := os.WriteFile(interactionPath, testWAV(70), 0o600); err != nil {
		t.Fatal(err)
	}
	// ProbeExternalWAV is HTTP based, so serve the interaction via a tiny local server.
	server := newWAVServer(t, interactionPath)
	defer server.Close()

	start, err := client.StartTestProgram(context.Background(), 15, "s15", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if start.Task == nil {
		t.Fatal("missing mainline task")
	}
	time.Sleep(25 * time.Millisecond)
	snapshot, err := client.InsertTestProgramInteraction(context.Background(), audioout.InsertInteractionInput{
		RoomID:    15,
		SessionID: "s15",
		Label:     "answer",
		AudioURL:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Suspended || snapshot.Task == nil || snapshot.Task.Kind != "interaction_audio" {
		t.Fatalf("interaction not active: %#v", snapshot)
	}
	interactionID := snapshot.Task.ID
	snapshot, err = client.CompleteProgramInteraction(context.Background(), 15, interactionID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Suspended || snapshot.Task == nil || snapshot.Task.Kind != "test_wav_program" || snapshot.Task.ID == interactionID {
		t.Fatalf("mainline did not resume: %#v", snapshot)
	}
	if active := hub.ActiveTask(15); active == nil || active.ID != snapshot.Task.ID {
		t.Fatalf("hub did not resume mainline: %#v", active)
	}
}

func TestSlowestMainlinePlaybackPositionUsesLaggingActiveOutput(t *testing.T) {
	tests := []struct {
		name           string
		wallClockMS    int
		receiverMS     int
		hasReceiver    bool
		roomAudioMS    int
		hasRoomAudio   bool
		durationMS     int
		wantPositionMS int
	}{
		{
			name:        "room audio mirror lags receiver",
			wallClockMS: 290000, receiverMS: 288000, hasReceiver: true,
			roomAudioMS: 250000, hasRoomAudio: true, durationMS: 600000,
			wantPositionMS: 250000,
		},
		{
			name:        "receiver lags room audio mirror",
			wallClockMS: 290000, receiverMS: 240000, hasReceiver: true,
			roomAudioMS: 275000, hasRoomAudio: true, durationMS: 600000,
			wantPositionMS: 240000,
		},
		{
			name:        "wall clock fallback without active output",
			wallClockMS: 42000, durationMS: 600000,
			wantPositionMS: 42000,
		},
		{
			name:        "position is clamped inside track",
			wallClockMS: 610000, durationMS: 600000,
			wantPositionMS: 599999,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slowestMainlinePlaybackPosition(
				tt.wallClockMS,
				tt.receiverMS,
				tt.hasReceiver,
				tt.roomAudioMS,
				tt.hasRoomAudio,
				tt.durationMS,
			)
			if got != tt.wantPositionMS {
				t.Fatalf("position=%d want=%d", got, tt.wantPositionMS)
			}
		})
	}
}

func TestProgramPauseResumeKeepsPlaybackCursor(t *testing.T) {
	client, hub := newTestClient(t, 420)
	controls, cancelControls := hub.SubscribeControls(18)
	defer cancelControls()
	start, err := client.StartTestProgram(context.Background(), 18, "s18", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if start.Task == nil {
		t.Fatal("missing mainline task")
	}
	time.Sleep(55 * time.Millisecond)
	paused, err := client.PauseProgram(context.Background(), 18)
	if err != nil {
		t.Fatal(err)
	}
	if !paused.Running || !paused.Suspended || paused.CurrentMS < 30 {
		t.Fatalf("unexpected paused snapshot: %#v", paused)
	}
	frozenMS := paused.CurrentMS
	select {
	case control := <-controls:
		if control.Action != "pause" || control.SpeechTaskID != start.Task.ID || control.PositionMS != frozenMS {
			t.Fatalf("unexpected pause control: %#v frozen=%d", control, frozenMS)
		}
	case <-time.After(time.Second):
		t.Fatal("pause did not broadcast receiver control")
	}
	time.Sleep(70 * time.Millisecond)
	stillPaused, err := client.ProgramSnapshot(context.Background(), 18)
	if err != nil {
		t.Fatal(err)
	}
	if !stillPaused.Suspended || stillPaused.CurrentMS != frozenMS {
		t.Fatalf("pause cursor moved: frozen=%d snapshot=%#v", frozenMS, stillPaused)
	}
	resumed, err := client.ResumeProgram(context.Background(), 18)
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Suspended || resumed.Task == nil || resumed.CurrentMS < frozenMS-5 {
		t.Fatalf("program did not resume from cursor %d: %#v", frozenMS, resumed)
	}
	time.Sleep(35 * time.Millisecond)
	after, err := client.ProgramSnapshot(context.Background(), 18)
	if err != nil {
		t.Fatal(err)
	}
	if after.CurrentMS <= frozenMS {
		t.Fatalf("resumed cursor did not advance: frozen=%d after=%#v", frozenMS, after)
	}
}

func TestNormalizeProgramTimelineRejectsCommaAsSafeCut(t *testing.T) {
	timeline, err := normalizeProgramTimeline([]audioout.ProgramTimelineSegment{
		{SegmentID: "A-001", Index: 1, StartMS: 0, EndMS: 1000, Text: "先把产品信息说清楚，", SafeCut: true},
		{SegmentID: "A-002", Index: 2, StartMS: 1000, EndMS: 2000, Text: "这一句到这里完整结束。", SafeCut: true},
	}, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if timeline[0].SafeCut {
		t.Fatalf("comma chunk must not remain a safe cut: %#v", timeline[0])
	}
	if !timeline[1].SafeCut {
		t.Fatalf("sentence ending should remain a safe cut: %#v", timeline[1])
	}
}

func TestPublishedProgramRotatesFormalTracks(t *testing.T) {
	client, _ := newTestClient(t, 100)
	trackAPath := filepath.Join(t.TempDir(), "a.wav")
	trackBPath := filepath.Join(t.TempDir(), "b.wav")
	if err := os.WriteFile(trackAPath, testWAV(70), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trackBPath, testWAV(90), 0o600); err != nil {
		t.Fatal(err)
	}
	serverA := newWAVServer(t, trackAPath)
	defer serverA.Close()
	serverB := newWAVServer(t, trackBPath)
	defer serverB.Close()

	start, err := client.StartProgram(context.Background(), audioout.StartProgramInput{
		RoomID:    21,
		SessionID: "live-runtime-99",
		Label:     "直播智能体 V7 主线",
		VersionID: 107,
		VersionNo: 7,
		Tracks: []audioout.ProgramTrack{
			{ID: "A", Label: "A稿", AudioURL: serverA.URL, DurationMS: 70},
			{ID: "B", Label: "B稿", AudioURL: serverB.URL, DurationMS: 90},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !start.Running || start.VersionID != 107 || start.VersionNo != 7 || start.TrackCount != 2 || start.TrackID != "A" {
		t.Fatalf("unexpected published program start: %#v", start)
	}
	time.Sleep(100 * time.Millisecond)
	next, err := client.ProgramSnapshot(context.Background(), 21)
	if err != nil {
		t.Fatal(err)
	}
	if !next.Running || next.TrackID != "B" || next.VersionNo != 7 || next.Task == nil || next.Task.AudioURL != serverB.URL {
		t.Fatalf("published program did not rotate to B track: %#v", next)
	}
}
