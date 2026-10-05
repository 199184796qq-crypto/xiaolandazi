package roomaudio

import "testing"

func TestPermanentRoomCleanupRejectsLateAudio(t *testing.T) {
	e := New()
	if _, err := e.StartMainline(15, "segment"); err != nil {
		t.Fatal(err)
	}
	frames, cancel, err := e.Subscribe(15)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	e.ClearRoom(15)
	e.Reset(15) // A subsequent normal stop must not clear the deletion marker.
	if _, ok := <-frames; ok {
		t.Fatal("PCM subscriber remains open")
	}
	if _, err := e.PublishPCM(15, SourceMainline, make([]byte, PCMBytesPerFrame), "late"); err == nil {
		t.Fatal("late PCM recreated the deleted room")
	}
	if _, err := e.StartMainline(15, "late"); err == nil {
		t.Fatal("late start recreated the deleted room")
	}
	e.SetMainlineTimeline(15, []SpeechSegment{{SegmentID: "late", Text: "late", EndMS: 20}})
	if _, _, err := e.Subscribe(15); err == nil {
		t.Fatal("deleted room stream rejoined")
	}
	if e.Metrics().Rooms != 0 {
		t.Fatal("deleted room retained runtime state")
	}
	if _, err := e.StartMainline(16, "unrelated"); err != nil {
		t.Fatal("cleanup affected another room")
	}
}
