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
	if !snapshot.Suspended || snapshot.Task == nil || snapshot.Task.Kind != "interaction_tts" {
		t.Fatalf("interaction not active: %#v", snapshot)
	}
	interactionID := snapshot.Task.ID
	time.Sleep(110 * time.Millisecond)
	snapshot, err = client.ProgramSnapshot(context.Background(), 15)
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
