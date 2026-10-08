package httpapi

import (
	"encoding/json"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"strings"
	"testing"
)

func TestProductionPreviewProjectionDoesNotExposeModelInstructions(t *testing.T) {
	state := cycleTestState(t)
	state.Surface = "generation_preview"
	state.Config.AgentID = "hidden-agent"
	state.Config.CredentialID = "hidden-key-profile"
	state.Accepted = []anchorcycle.Segment{{Selected: anchorcycle.Candidate{Text: "采纳的话术", Prompt: "hidden-prompt", RawText: "hidden-raw"}, Feedback: json.RawMessage(`{"request":"hidden-feedback"}`)}}
	raw, _ := json.Marshal(cyclePreviewProjection(&state))
	for _, secret := range []string{"hidden-agent", "hidden-key-profile", "hidden-prompt", "hidden-raw", "hidden-feedback", state.StyleMD, state.Facts} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("projection leaked", secret)
		}
	}
	if !strings.Contains(string(raw), "采纳的话术") {
		t.Fatal("lost speech")
	}
}

func TestProductionPreviewProjectionKeepsEmptyAcceptedAsArray(t *testing.T) {
	state := cycleTestState(t)
	state.Surface = "generation_preview"
	state.Accepted = []anchorcycle.Segment{}
	raw, err := json.Marshal(cyclePreviewProjection(&state))
	if err != nil {
		t.Fatal(err)
	}
	var projected map[string]any
	if err := json.Unmarshal(raw, &projected); err != nil {
		t.Fatal(err)
	}
	accepted, ok := projected["accepted"].([]any)
	if !ok || accepted == nil || len(accepted) != 0 {
		t.Fatalf("accepted must be an empty JSON array, got %#v", projected["accepted"])
	}
}

func TestPreviewLimitCannotBeUsedForUnlimitedRoomAudio(t *testing.T) {
	state := cycleTestState(t)
	state.PreviewLimitMS = 120000
	state.Accepted = []anchorcycle.Segment{{Voice: &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 80000}}}, {Voice: &anchorcycle.VoiceAsset{Status: "ready", Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 40000}}}}
	if !previewAudioComplete(&state) {
		t.Fatal("failed to stop at limit")
	}
	state.Accepted = []anchorcycle.Segment{{Voice: &anchorcycle.VoiceAsset{Status: "ready", PreviewTruncated: true, Bundle: model.LiveAgentPlanVersionVariant{AudioDurationMS: 70000}}}}
	if previewAudioComplete(&state) {
		t.Fatal("truncation marker ended preview before actual 2-minute budget")
	}
	state.PreviewLimitMS = 0
	if previewAudioComplete(&state) {
		t.Fatal("limited formal/unlimited operation run")
	}
}
func TestLimitedCacheCannotPoisonFormalVoiceCache(t *testing.T) {
	state := cycleTestState(t)
	segment := anchorcycle.Segment{Task: state.Queue[0]}
	full := cycleVoiceCacheGroup(&state, segment)
	state.PreviewLimitMS = 120000
	if full == cycleVoiceCacheGroup(&state, segment) {
		t.Fatal("preview and formal namespaces overlap")
	}
}
