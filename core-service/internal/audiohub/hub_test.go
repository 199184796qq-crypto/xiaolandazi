package audiohub

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
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

func TestCreateExternalTaskBroadcastsAndLateJoinSeeks(t *testing.T) {
	wav := testWAV(220)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}))
	defer source.Close()

	hub := New()
	ch, latest, cancel := hub.Subscribe(15)
	defer cancel()
	if latest != nil {
		t.Fatalf("unexpected task before publish: %#v", latest)
	}

	task, err := hub.CreateExternalTask(context.Background(), 15, "control-15", "answer", source.URL)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		if got.ID != task.ID || got.StartMS != 0 {
			t.Fatalf("unexpected broadcast: %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("task was not broadcast")
	}

	time.Sleep(60 * time.Millisecond)
	_, late, cancelLate := hub.Subscribe(15)
	defer cancelLate()
	if late == nil || late.ID != task.ID {
		t.Fatalf("late subscriber missing task: %#v", late)
	}
	if late.StartMS < 25 || late.StartMS >= late.DurationMS {
		t.Fatalf("late join offset invalid: %#v", late)
	}
}

func TestReceiverCompletionDoesNotControlTaskLifecycle(t *testing.T) {
	wav := testWAV(90)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(wav)
	}))
	defer source.Close()

	hub := New()
	task, err := hub.CreateExternalTask(context.Background(), 9, "s9", "test", source.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.ReportEvent(task.ID, PlaybackEvent{
		ReceiverID: "pc-a",
		Status:     "COMPLETED",
		ProgressMS: task.DurationMS,
	}); err != nil {
		t.Fatal(err)
	}
	if snapshot, ok := hub.Snapshot(task.ID); !ok || snapshot.Terminal {
		t.Fatalf("receiver completion ended task: %#v", snapshot)
	}
	time.Sleep(130 * time.Millisecond)
	if snapshot, ok := hub.Snapshot(task.ID); !ok || !snapshot.Terminal {
		t.Fatalf("server clock did not end task: %#v", snapshot)
	}
}
