package httpapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

func playbackIdentity(id string) model.LiveAgentVoiceIdentity {
	return model.LiveAgentVoiceIdentity{Name: id, Version: "V1", Source: "official", Provider: "aliyun_qwen", TTSPool: "standard", Model: "qwen-audio-3.1-tts-flash", VoiceID: id, Rate: 1.13}
}

func playbackSettingsInput() model.CreateLiveAgentPlanVersionInput {
	a, b := playbackIdentity("A"), playbackIdentity("B")
	return model.CreateLiveAgentPlanVersionInput{VoiceIdentity: a, GenerationContext: map[string]any{"recording_script": recordingScript{
		Version: 2, PlayMode: "single", VersionCap: 7, MinVoiceBlock: 4,
		Tabs:   []recordingScriptTab{{TabID: "a", Entries: []recordingScriptEntry{{EntryID: "1", Text: longText("a")}, {EntryID: "2", Text: longText("b")}}}},
		Voices: []recordingScriptVoice{{VoiceID: a.VoiceID, Model: a.Model, Identity: &a}, {VoiceID: b.VoiceID, Model: b.Model, Identity: &b}},
	}}}
}

func TestScriptPlaybackSelectionRoundTripAndActualIdentity(t *testing.T) {
	input := playbackSettingsInput()
	calls := 0
	if err := normalizeScriptPlaybackVoices(&input, func(identity *model.LiveAgentVoiceIdentity) error {
		calls++
		return normalizeLiveAgentVoiceIdentity(identity)
	}); err != nil || calls != 2 {
		t.Fatal("every selection must be authorized", err, calls)
	}
	raw, _ := json.Marshal(input.GenerationContext)
	var restored map[string]any
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	script, ok := recordingScriptFromContext(restored)
	if !ok || script.EffectivePlayMode() != "single" || len(script.Voices) != 2 || script.VersionCap != 7 || script.MinVoiceBlock != 4 {
		t.Fatal(script)
	}
	v := model.LiveAgentPlanVersion{VoiceIdentity: input.VoiceIdentity, GenerationContext: restored}
	secondary := scriptVersionForVoice(v, script.Voices[1])
	if secondary.VoiceIdentity.VoiceID != "B" || secondary.VoiceIdentity.Name != "B" || secondary.VoiceIdentity.Source != "official" || secondary.VoiceIdentity.TTSPool != "standard" || secondary.VoiceIdentity.Rate != 1.13 {
		t.Fatal(secondary.VoiceIdentity)
	}
}

func TestScriptPlaybackRejectsInvalidSelections(t *testing.T) {
	for _, scenario := range []string{"missing", "mismatch", "disabled", "cross-pool", "primary", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			input := playbackSettingsInput()
			script := input.GenerationContext["recording_script"].(recordingScript)
			switch scenario {
			case "missing":
				script.Voices[1].Identity = nil
			case "mismatch":
				script.Voices[1].Identity.VoiceID = "fake"
			case "cross-pool":
				script.Voices[1].Identity.Provider, script.Voices[1].Identity.TTSPool, script.Voices[1].Identity.Model = "volcengine", "high_fidelity", "seed-icl-2.0"
				script.Voices[1].Model = "seed-icl-2.0"
			case "primary":
				script.Voices[0], script.Voices[1] = script.Voices[1], script.Voices[0]
			case "unsupported":
				script.Voices[1].Identity.Model, script.Voices[1].Model = "unsupported", "unsupported"
			}
			input.GenerationContext["recording_script"] = script
			err := normalizeScriptPlaybackVoices(&input, func(identity *model.LiveAgentVoiceIdentity) error {
				if scenario == "disabled" && identity.VoiceID == "B" {
					return errors.New("已停用")
				}
				return normalizeLiveAgentVoiceIdentity(identity)
			})
			if err == nil {
				t.Fatal("invalid selection accepted")
			}
		})
	}
	input := playbackSettingsInput()
	script := input.GenerationContext["recording_script"].(recordingScript)
	script.Voices = []recordingScriptVoice{{VoiceID: input.VoiceIdentity.VoiceID}}
	input.GenerationContext["recording_script"] = script
	if err := normalizeScriptPlaybackVoices(&input, normalizeLiveAgentVoiceIdentity); err != nil {
		t.Fatal("legacy primary should restore", err)
	}
	for _, mode := range []string{"full", "single"} {
		script.PlayMode = mode
		if err := script.validate(); err != nil {
			t.Fatal(err)
		}
	}
	script.PlayMode = "invalid"
	if err := script.validate(); err == nil {
		t.Fatal("invalid mode accepted")
	}
	script.PlayMode, script.Voices = "full", make([]recordingScriptVoice, 9)
	if err := script.validate(); err == nil || !strings.Contains(err.Error(), "8") {
		t.Fatal("too many voices accepted", err)
	}
}

func TestScriptPreparesOnlyMissingNextVoiceAndRequiresAllCache(t *testing.T) {
	v, units, root := voiceFixture(t)
	a, b, c := playbackIdentity("A"), playbackIdentity("B"), playbackIdentity("C")
	v.VoiceIdentity = a
	voices := []recordingScriptVoice{{VoiceID: "A", Model: a.Model, Identity: &a}, {VoiceID: "B", Model: b.Model, Identity: &b}, {VoiceID: "C", Model: c.Model, Identity: &c}}
	for _, unit := range units {
		addReadyScriptFixture(t, root, v, unit, "A", 101)
	}
	var lastPath string
	for i := range units {
		target, unit, needed := scriptNextVoicePreparation(voices, 0, v, units)
		if !needed || target.VoiceIdentity.VoiceID != "B" || unit.poolID() != units[i].poolID() || unit.Key == units[i].Key {
			t.Fatal("wrong voice/piece/cache", target, unit, needed)
		}
		if _, ready := scriptNextPlaybackVoice(voices, 0, v, units); ready {
			t.Fatal("switched before all pieces ready")
		}
		version := addReadyScriptFixture(t, root, target, unit, "B", int64(200+i))
		lastPath = filepath.Join(version.SourcePath, "cache.json")
	}
	if _, _, needed := scriptNextVoicePreparation(voices, 0, v, units); needed {
		t.Fatal("ready voice should not buy again")
	}
	if next, ready := scriptNextPlaybackVoice(voices, 0, v, units); !ready || next != 1 {
		t.Fatal(next, ready)
	}
	if err := os.Remove(lastPath); err != nil {
		t.Fatal(err)
	}
	if _, ready := scriptNextPlaybackVoice(voices, 0, v, units); ready {
		t.Fatal("missing paid cache passed readiness")
	}
	if target, _, needed := scriptNextVoicePreparation(voices, 0, v, units); !needed || target.VoiceIdentity.VoiceID != "B" {
		t.Fatal("must repair B before buying C")
	}
	if _, _, needed := scriptNextVoicePreparation(voices[:1], 0, v, units); needed {
		t.Fatal("single voice should not prepare another")
	}
	voices[1].Identity = nil
	if _, _, needed := scriptNextVoicePreparation(voices, 0, v, units); needed {
		t.Fatal("legacy unproven secondary must not buy")
	}
}

func TestScriptVoicePreparationProtectsCurrentSupplyAndPaidLead(t *testing.T) {
	cursor := volcengineScriptCursor{ProgramID: "live", Round: 3, Position: 2, VoiceBlockLeft: 1}
	for _, test := range []struct {
		ms                  int64
		waiting, ready, due bool
		last                string
	}{
		{40000, false, true, false, ""}, {39999, false, true, true, ""},
		{15000, false, true, true, ""}, {14999, false, true, false, ""},
		{30000, true, true, false, ""}, {30000, false, false, false, ""},
		{30000, false, true, false, "3:2"},
	} {
		if got := scriptVoicePreparationDue(cursor, coreclient.ProgramRefreshSnapshot{BufferedMainlineMS: test.ms, WaitingForContent: test.waiting}, test.last, test.ready); got != test.due {
			t.Fatal(test, got)
		}
	}
	cursor.ProgramID = ""
	if scriptVoicePreparationDue(cursor, coreclient.ProgramRefreshSnapshot{BufferedMainlineMS: 30000}, "", true) {
		t.Fatal("must supply first audio before preparing another voice")
	}
	cursor.ProgramID, cursor.VoiceBlockLeft = "live", 2
	if scriptVoicePreparationDue(cursor, coreclient.ProgramRefreshSnapshot{BufferedMainlineMS: 30000}, "", true) {
		t.Fatal("must not buy other voices too early")
	}
}

func TestScriptVoiceUsesSelectedCloneBindingInsteadOfPrimary(t *testing.T) {
	primary, secondary := playbackIdentity("A"), playbackIdentity("B")
	primary.Source, primary.ProfileID, primary.BindingID = "clone", 10, 20
	secondary.Source, secondary.ProfileID, secondary.BindingID = "clone", 11, 21
	secondary.Rate = 0.9
	v := scriptVersionForVoice(model.LiveAgentPlanVersion{VoiceIdentity: primary}, recordingScriptVoice{VoiceID: "B", Model: secondary.Model, Identity: &secondary})
	if v.VoiceIdentity.ProfileID != 11 || v.VoiceIdentity.BindingID != 21 || v.VoiceIdentity.VoiceID != "B" || v.VoiceIdentity.Rate != primary.Rate {
		t.Fatal(v.VoiceIdentity)
	}
	if primary.VoiceID != "A" || secondary.Rate != 0.9 {
		t.Fatal("must not mutate source identities")
	}
	input := playbackSettingsInput()
	script := input.GenerationContext["recording_script"].(recordingScript)
	script.Voices[1].Identity = &secondary
	input.GenerationContext["recording_script"] = script
	// The actual PCM receipt path reconstructs a secondary voice from model/id.
	receiptVersion := scriptVersionForVoice(model.LiveAgentPlanVersion{VoiceIdentity: primary, GenerationContext: input.GenerationContext}, recordingScriptVoice{VoiceID: "B", Model: secondary.Model})
	if receiptVersion.VoiceIdentity.ProfileID != 11 || receiptVersion.VoiceIdentity.BindingID != 21 || receiptVersion.VoiceIdentity.VoiceID != "B" {
		t.Fatal("receipt reused the first voice's binding", receiptVersion.VoiceIdentity)
	}
}
