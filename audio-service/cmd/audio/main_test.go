package main

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTestChimeWAV(t *testing.T) {
	audio := testChimeWAV(1800)
	if len(audio) <= 44 {
		t.Fatalf("wav too short: %d", len(audio))
	}
	if string(audio[:4]) != "RIFF" || string(audio[8:12]) != "WAVE" {
		t.Fatalf("not a RIFF/WAVE file")
	}
	if got := binary.LittleEndian.Uint32(audio[24:28]); got != 24000 {
		t.Fatalf("sample rate=%d", got)
	}
}

func TestWAVDurationMS(t *testing.T) {
	audio := testChimeWAV(1800)
	duration, err := wavDurationMS(audio)
	if err != nil {
		t.Fatal(err)
	}
	if duration != 1800 {
		t.Fatalf("duration=%d want=1800", duration)
	}
}

func TestWAVDurationMSAcceptsStreamingDataPlaceholder(t *testing.T) {
	audio := testChimeWAV(1800)
	binary.LittleEndian.PutUint32(audio[4:8], 0x7fffffbf)
	binary.LittleEndian.PutUint32(audio[40:44], 0x7fffff9b)

	duration, err := wavDurationMS(audio)
	if err != nil {
		t.Fatal(err)
	}
	if duration != 1800 {
		t.Fatalf("duration=%d want=1800", duration)
	}
}

func TestBrokerUsesConfiguredTestWAV(t *testing.T) {
	audio := testChimeWAV(3100)
	broker, err := NewBrokerWithTestAudio("http://127.0.0.1:8082", "", audio, 3100, "configured.wav")
	if err != nil {
		t.Fatal(err)
	}
	task, err := broker.CreateTestTask(66, "configured", "configured-task", "", 800, "")
	if err != nil {
		t.Fatal(err)
	}
	if task.Kind != "test_wav" || task.DurationMS != 3100 {
		t.Fatalf("configured WAV not used: %#v", task)
	}
	got, _, ok := broker.Audio(task.ID)
	if !ok || len(got) != len(audio) {
		t.Fatalf("configured audio missing")
	}
}

func TestBrokerFansOneTaskToTwoReceivers(t *testing.T) {
	broker := NewBroker("http://127.0.0.1:8082", "")
	a, _, cancelA := broker.Subscribe(77)
	defer cancelA()
	b, _, cancelB := broker.Subscribe(77)
	defer cancelB()

	task, err := broker.CreateTestTask(77, "session-1", "task-1", "test", 1200, "")
	if err != nil {
		t.Fatal(err)
	}
	for name, ch := range map[string]<-chan SpeechTask{"a": a, "b": b} {
		select {
		case got := <-ch:
			if got.ID != task.ID || got.AudioURL != task.AudioURL {
				t.Fatalf("%s got different task: %#v", name, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive task", name)
		}
	}
	audio, _, ok := broker.Audio(task.ID)
	if !ok || len(audio) <= 44 {
		t.Fatalf("shared generated audio missing")
	}
}

func TestCompletedTaskIsNotReplayedToReconnect(t *testing.T) {
	broker := NewBroker("http://127.0.0.1:8082", "")
	task, err := broker.CreateTestTask(88, "session-2", "task-done", "done", 800, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ref, err := broker.ReportEvent(task.ID, PlaybackEvent{ReceiverID: "pc-a", Status: "READY"}); err != nil || !ref {
		t.Fatalf("reference ready: ref=%v err=%v", ref, err)
	}
	if _, ref, err := broker.ReportEvent(task.ID, PlaybackEvent{ReceiverID: "pc-a", Status: "COMPLETED", ProgressMS: 800}); err != nil || !ref {
		t.Fatalf("reference completed: ref=%v err=%v", ref, err)
	}
	_, latest, cancel := broker.Subscribe(88)
	defer cancel()
	if latest != nil {
		t.Fatalf("completed task replayed on reconnect: %#v", latest)
	}
	snapshot, ok := broker.Snapshot(task.ID)
	if !ok || !snapshot.Terminal {
		t.Fatalf("completed task not marked terminal: %#v", snapshot)
	}
}

func TestProgramCanInsertExternalWAVAndResumeMainline(t *testing.T) {
	mainline := testChimeWAV(500)
	broker, err := NewBrokerWithTestAudio("http://audio.local", "", mainline, 500, "mainline.wav")
	if err != nil {
		t.Fatal(err)
	}

	interactionAudio := testChimeWAV(120)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(interactionAudio)
	}))
	defer source.Close()

	ch, _, cancelSub := broker.Subscribe(301)
	defer cancelSub()

	start, err := broker.StartTestProgram(301, "session-301", "mainline", "")
	if err != nil {
		t.Fatal(err)
	}
	if start.Task == nil {
		t.Fatal("start snapshot missing task")
	}
	select {
	case first := <-ch:
		if first.Kind != "test_wav_program" || first.Slot != "A" {
			t.Fatalf("unexpected first task: %#v", first)
		}
	case <-time.After(time.Second):
		t.Fatal("mainline task not published")
	}

	time.Sleep(35 * time.Millisecond)
	snapshot, err := broker.InsertExternalWAV(
		context.Background(),
		301,
		"session-301",
		"real tts interaction",
		source.URL,
		"",
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Suspended || snapshot.Task == nil || snapshot.Task.Kind != "interaction_tts" {
		t.Fatalf("interaction snapshot=%#v", snapshot)
	}
	if snapshot.Task.Slot != "B" {
		t.Fatalf("interaction should use B slot, got %q", snapshot.Task.Slot)
	}
	interactionSequence := snapshot.Task.Sequence

	select {
	case interaction := <-ch:
		if interaction.Kind != "interaction_tts" {
			t.Fatalf("unexpected interaction task: %#v", interaction)
		}
		if interaction.AudioURL == source.URL {
			t.Fatal("receiver must use cached audio-service URL, not provider URL")
		}
	case <-time.After(time.Second):
		t.Fatal("interaction task not published")
	}

	select {
	case resumed := <-ch:
		if resumed.Kind != "test_wav_program" {
			t.Fatalf("unexpected resumed task: %#v", resumed)
		}
		if resumed.Sequence != interactionSequence+1 {
			t.Fatalf("resume sequence=%d want=%d", resumed.Sequence, interactionSequence+1)
		}
		if resumed.Slot != "A" {
			t.Fatalf("resumed mainline should return to A slot, got %q", resumed.Slot)
		}
		if resumed.StartMS < 20 {
			t.Fatalf("expected resumed mainline near interrupted offset, start_ms=%d", resumed.StartMS)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("mainline was not resumed after interaction")
	}

	finalSnapshot, ok := broker.ProgramSnapshot(301)
	if !ok || finalSnapshot.Suspended || finalSnapshot.Task == nil || finalSnapshot.Task.Kind != "test_wav_program" {
		t.Fatalf("final snapshot=%#v", finalSnapshot)
	}
}

func TestProgramMainlineUsesVersionedAudioURL(t *testing.T) {
	audio := testChimeWAV(1200)
	broker, err := NewBrokerWithTestAudio("http://audio.local", "", audio, 1200, "mainline.wav")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := broker.StartTestProgram(901, "s901", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Task == nil {
		t.Fatal("missing task")
	}
	if !strings.Contains(snapshot.Task.AudioURL, "/v1/test-audio.wav?v=") {
		t.Fatalf("mainline URL is not versioned: %s", snapshot.Task.AudioURL)
	}
}

func TestTestAudioSupportsRangeAndDisablesCache(t *testing.T) {
	audio := testChimeWAV(1200)
	broker, err := NewBrokerWithTestAudio("http://audio.local", "", audio, 1200, "mainline.wav")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(newServer(broker, "").handler())
	defer server.Close()

	req, err := http.NewRequest(http.MethodGet, server.URL+"/v1/test-audio.wav?v="+broker.testAudioVer, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-99")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status=%d want=%d", resp.StatusCode, http.StatusPartialContent)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache-control=%q", got)
	}
	if got := resp.Header.Get("Content-Range"); got == "" {
		t.Fatal("missing Content-Range")
	}
}

func TestOnlyReferenceReceiverForwardsRoomPlayback(t *testing.T) {
	var mu sync.Mutex
	var callbacks []PlaybackEvent
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Core-Token") != "core-secret" {
			t.Errorf("missing core token")
		}
		var event PlaybackEvent
		if err := readJSON(w, r, &event); err != nil {
			t.Errorf("callback decode: %v", err)
		}
		mu.Lock()
		callbacks = append(callbacks, event)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer core.Close()

	broker := NewBroker("http://audio.local", "core-secret")
	task, err := broker.CreateTestTask(9, "s1", "t1", "test", 900, core.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, ref, err := broker.ReportEvent(task.ID, PlaybackEvent{ReceiverID: "pc-a", Status: "READY"}); err != nil || !ref {
		t.Fatalf("first receiver should become reference: ref=%v err=%v", ref, err)
	}
	if _, ref, err := broker.ReportEvent(task.ID, PlaybackEvent{ReceiverID: "pc-b", Status: "COMPLETED"}); err != nil || ref {
		t.Fatalf("second receiver must not advance room: ref=%v err=%v", ref, err)
	}
	if _, ref, err := broker.ReportEvent(task.ID, PlaybackEvent{ReceiverID: "pc-a", Status: "COMPLETED", ProgressMS: 900}); err != nil || !ref {
		t.Fatalf("reference completion should forward: ref=%v err=%v", ref, err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(callbacks)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(callbacks) != 2 {
		t.Fatalf("callbacks=%d want=2 (READY + COMPLETED from reference only)", len(callbacks))
	}
	if callbacks[1].Status != "COMPLETED" || callbacks[1].ReceiverID != "pc-a" {
		t.Fatalf("unexpected completion callback: %#v", callbacks[1])
	}
}
