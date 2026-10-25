package httpapi

import (
	"context"
	"errors"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

func TestRecordingUpdatePercentByArticleLength(t *testing.T) {
	if got := recordingUpdatePercent(1000); got != 25 {
		t.Fatalf("short article percent=%d, want 25", got)
	}
	if got := recordingUpdatePercent(6000); got != 20 {
		t.Fatalf("medium article percent=%d, want 20", got)
	}
	if got := recordingUpdatePercent(15000); got != 10 {
		t.Fatalf("long article percent=%d, want 10", got)
	}
}

func TestRecordingCacheLookupKeepsValidOriginal(t *testing.T) {
	manifest := &recordingcache.Manifest{Entries: []recordingcache.Entry{{
		Sentence: "原句", AssetID: 7, Polished: false,
		Segments: []recordingcache.Segment{{Text: "原句", StartMS: 0, EndMS: 1000, SafeCut: true}},
	}}}
	hit := recordingCacheLookup(manifest)
	if len(hit["原句"]) != 1 {
		t.Fatalf("original A entry must be reusable, got %+v", hit)
	}
}

func TestRecordingBudgetStopsOptionalUpdates(t *testing.T) {
	if got := recordingBudgetPercent(recordingBudgetSoftCapMicros-1, 25); got != 25 {
		t.Fatalf("under cap should keep optional updates, got %d", got)
	}
	if got := recordingBudgetPercent(recordingBudgetSoftCapMicros, 25); got != 0 {
		t.Fatalf("at cap should stop optional updates, got %d", got)
	}
	if got := recordingBudgetPercent(recordingBudgetSoftCapMicros+1, 0); got != 0 {
		t.Fatalf("zero desired must stay zero, got %d", got)
	}
}

func TestRecordingOriginalP0RejectionUsesTypedError(t *testing.T) {
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	state := &anchorcycle.State{P0PolicyMD: md}
	server := &Server{}
	seq := 0
	_, err = server.compileRecordingOriginal(context.Background(), 1, model.LiveAgentPlanVersion{}, state, "咱们家全网最好，绝对第一", 0, &seq)
	if !errors.Is(err, errRecordingOriginalRejected) {
		t.Fatalf("P0 rejection must be typed for controlled fallback, got %v", err)
	}
}
