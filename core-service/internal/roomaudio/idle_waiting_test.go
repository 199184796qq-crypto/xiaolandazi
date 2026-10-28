package roomaudio

import "testing"

func TestIdleWaitingContentKeepsSubscribersAndOutputClock(t *testing.T) {
	engine := New()
	if _, err := engine.StartMainline(91, "mainline"); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PublishPCMAt(91, SourceMainline, make([]byte, PCMBytesPerFrame), "mainline", 20); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.StartInterrupt(91); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.PublishPCMAt(91, SourceInterrupt, make([]byte, PCMBytesPerFrame), "answer", 20); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Subscribe(91); err != nil {
		t.Fatal(err)
	}
	before := engine.Snapshot(91)
	snapshot := engine.IdleWaitingContent(91)
	if snapshot.Phase != PhaseIdle || snapshot.Subscribers != 1 {
		t.Fatalf("idle transition lost state: %+v", snapshot)
	}
	if snapshot.OutputPTSMS < before.OutputPTSMS {
		t.Fatalf("output clock moved backwards: before=%d after=%d", before.OutputPTSMS, snapshot.OutputPTSMS)
	}
	if snapshot.MainlineCursorMS != before.MainlineCursorMS {
		t.Fatalf("mainline position lost: before=%d after=%d", before.MainlineCursorMS, snapshot.MainlineCursorMS)
	}
	if _, err := engine.StartMainline(91, "next"); err != nil {
		t.Fatalf("room could not resume after waiting-content idle: %v", err)
	}
}
