package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/contentstrategy"
	"testing"
)

func TestContentStrategySeparatesOldSpeechCache(t *testing.T) {
	state := cycleTestState(t)
	segment := anchorcycle.Segment{Phase: "body", Task: state.Task()}
	old := cycleVoiceCacheGroup(&state, segment)
	state.ContentStrategy = &contentstrategy.Snapshot{Version: 1, Enabled: true, FactsSHA: "facts", Rows: []contentstrategy.Row{{Angle: "单只预算"}}}
	first := cycleVoiceCacheGroup(&state, segment)
	if first == old {
		t.Fatal("new strategy can replay old generated speech")
	}
	state.ContentStrategy.Rows[0].Angle = "斤价理解"
	if cycleVoiceCacheGroup(&state, segment) == first {
		t.Fatal("changed angle table uses stale voice cache")
	}
	state.ContentStrategy.Enabled = false
	if cycleVoiceCacheGroup(&state, segment) != old {
		t.Fatal("disabled strategy changed old cache identity")
	}
}
