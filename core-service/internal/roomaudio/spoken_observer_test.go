package roomaudio

import "testing"

func TestSpokenObserverOnlySeesAcceptedPCMNotPreparedOrFutureText(t *testing.T) {
	e := New()
	var got []SpokenSegment
	e.SetSpokenObserver(func(s SpokenSegment) { _ = e.Snapshot(s.RoomID); got = append(got, s) })
	e.SetMainlineTimeline(29, []SpeechSegment{{SegmentID: "one", Text: "想要就扣1。", StartMS: 0, EndMS: 100}, {SegmentID: "future", Text: "接下来扣2。", StartMS: 100, EndMS: 200}})
	if _, err := e.StartMainline(29, "one"); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatal("preparing a program activated the prompt")
	}
	for i := 0; i < 2; i++ {
		if _, err := e.PublishPCMAt(29, SourceMainline, make([]byte, PCMBytesPerFrame), "one", i*20); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 1 || got[0].Text != "想要就扣1。" || got[0].Source != SourceMainline || got[0].Sequence != 1 || got[0].RemainingMS != 100 {
		t.Fatal(got)
	}
	e.SetInterruptTimeline(29, []SpeechSegment{{SegmentID: "reply", Text: "把收到打在公屏上。", StartMS: 0, EndMS: 100}})
	if _, err := e.PrepareInterrupt(29); err != nil {
		t.Fatal(err)
	}
	if _, err := e.StartInterrupt(29); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("prepared interrupt announced before PCM")
	}
	if _, err := e.PublishPCMAt(29, SourceInterrupt, make([]byte, PCMBytesPerFrame), "reply", 0); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Text != "把收到打在公屏上。" || got[1].Source != SourceInterrupt {
		t.Fatal(got)
	}
	if _, err := e.PublishPCMAt(29, SourceMainline, make([]byte, PCMBytesPerFrame), "future", 100); err == nil {
		t.Fatal("wrong source accepted")
	}
	if len(got) != 2 {
		t.Fatal("rejected frame activated a prompt")
	}
}

func TestSpokenObserverCannotBreakForegroundAudio(t *testing.T) {
	e := New()
	e.SetSpokenObserver(func(SpokenSegment) { panic("observer failed") })
	e.SetMainlineTimeline(29, []SpeechSegment{{SegmentID: "one", Text: "扣1。", StartMS: 0, EndMS: 100}})
	e.StartMainline(29, "one")
	frame, err := e.PublishPCMAt(29, SourceMainline, make([]byte, PCMBytesPerFrame), "one", 0)
	if err != nil || frame.Sequence != 1 || len(frame.PCM) != PCMBytesPerFrame {
		t.Fatal(frame, err)
	}
}
