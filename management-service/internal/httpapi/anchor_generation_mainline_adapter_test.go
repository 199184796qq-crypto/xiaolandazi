package httpapi

import (
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"testing"
)

func TestRealtimeMainlineAdapterRejectsPreviewEvenWithReadyVoice(t *testing.T) {
	state := cycleTestState(t)
	state.TenantID = 1
	state.PlanID = 2
	state.RoomID = 3
	state.Surface = "generation_preview"
	if _, err := liveCycleMainlineTrack(&state, anchorcycle.Segment{}); err == nil {
		t.Fatal("preview entered Core")
	}
	state.Surface = "formal_live"
	state.PreviewLimitMS = 120000
	if _, err := liveCycleMainlineTrack(&state, anchorcycle.Segment{}); err == nil {
		t.Fatal("limited preview promoted to formal")
	}
	state.PreviewLimitMS = 0
	if _, err := liveCycleMainlineTrack(&state, anchorcycle.Segment{Selected: anchorcycle.Candidate{ID: "not-adopted"}}); err == nil {
		t.Fatal("unadopted draft entered Core")
	}
}

func TestRealtimeMainlineAdapterProducesSourceNeutralCompleteTrack(t *testing.T) {
	state := cycleTestState(t)
	state.TenantID = 1
	state.PlanID = 2
	state.RoomID = 3
	state.Surface = "formal_live"
	if err := installP0Mother(&state); err != nil {
		t.Fatal(err)
	}
	segment := anchorcycle.Segment{Task: state.Queue[0], Selected: anchorcycle.Candidate{ID: "adopted", Text: "已采纳。"}, Voice: &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{
		VariantKey: "mainline", Text: "已采纳。", AudioURL: "https://example.invalid/signed.wav", AudioDurationMS: 1000,
		Timeline:      []model.LiveAgentPlanTimelineSegment{{SegmentID: "s1", Index: 1, StartMS: 0, EndMS: 1000, Text: "已采纳。", SafeCut: true}},
		SRT:           "1\n00:00:00,000 --> 00:00:01,000\n已采纳。\n",
		SafePoints:    []model.LiveAgentPlanSafePoint{{ID: "cut1", CutMS: 1000, Score: 100, Grade: "A", Kind: "sentence", SentenceID: "s1"}},
		AssetManifest: map[string]any{"version": "bundle-v1"},
	}}}
	if err := state.CheckP0Candidate(&segment.Selected); err != nil {
		t.Fatal(err)
	}
	state.Accepted = []anchorcycle.Segment{segment}
	track, err := liveCycleMainlineTrack(&state, segment)
	if err != nil {
		t.Fatal(err)
	}
	if track.ID != "adopted" || track.DurationMS != 1000 || len(track.Timeline) != 1 || len(track.SafePoints) != 1 {
		t.Fatal("incomplete mainline track", track)
	}
}
