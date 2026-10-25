package httpapi

import (
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
)

func TestRecordingCurrentCacheIdentityDependencies(t *testing.T) {
	base := func() model.LiveAgentPlanVersion {
		return model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{Source: "profile", VoiceID: "voice-a", Model: "tts-a", Rate: 1, ProfileID: 7, BindingID: 8},
			GenerationContext: map[string]any{"facts": "原价337元", "derived_safety": map[string]any{"source_sha256": "facts-a"},
				"recording_script": recordingScript{Tabs: []recordingScriptTab{
					{TabID: "first", Title: "开场", Order: 1, Text: "欢迎朋友。"},
					{TabID: "second", Title: "介绍", Order: 2, Text: "请看商品。"},
				}}}}
	}
	policy := &anchorcycle.State{P0PolicyMD: "rules-a"}
	version := base()
	voice, content := recordingCurrentCacheIdentity(version, recordingStreamTexts(version), policy)
	for name, change := range map[string]func(*model.LiveAgentPlanVersion){
		"fact without safety revision": func(v *model.LiveAgentPlanVersion) { v.GenerationContext["facts"] = "原价338元" },
		"safety revision": func(v *model.LiveAgentPlanVersion) {
			v.GenerationContext["derived_safety"] = map[string]any{"source_sha256": "facts-b"}
		},
		"body": func(v *model.LiveAgentPlanVersion) {
			script := v.GenerationContext["recording_script"].(recordingScript)
			script.Tabs[0].Text = "大家好。"
			v.GenerationContext["recording_script"] = script
		},
	} {
		t.Run(name, func(t *testing.T) {
			v := base()
			change(&v)
			_, key := recordingCurrentCacheIdentity(v, recordingStreamTexts(v), policy)
			if key == content {
				t.Fatal("changed content still reuses old namespace")
			}
		})
	}
	for name, change := range map[string]func(*model.LiveAgentPlanVersion){
		"voice":   func(v *model.LiveAgentPlanVersion) { v.VoiceIdentity.VoiceID = "voice-b" },
		"source":  func(v *model.LiveAgentPlanVersion) { v.VoiceIdentity.Source = "custom" },
		"profile": func(v *model.LiveAgentPlanVersion) { v.VoiceIdentity.ProfileID++ },
		"binding": func(v *model.LiveAgentPlanVersion) { v.VoiceIdentity.BindingID++ },
		"model":   func(v *model.LiveAgentPlanVersion) { v.VoiceIdentity.Model = "tts-b" },
		"rate":    func(v *model.LiveAgentPlanVersion) { v.VoiceIdentity.Rate = .9 },
		"emotion": func(v *model.LiveAgentPlanVersion) { enabled := true; v.VoiceIdentity.EmotionEnabled = &enabled },
	} {
		t.Run(name, func(t *testing.T) {
			v := base()
			change(&v)
			key, _ := recordingCurrentCacheIdentity(v, recordingStreamTexts(v), policy)
			if key == voice {
				t.Fatal("changed voice still reuses old namespace")
			}
		})
	}
	_, changedPolicy := recordingCurrentCacheIdentity(version, recordingStreamTexts(version), &anchorcycle.State{P0PolicyMD: "rules-b"})
	if changedPolicy == content {
		t.Fatal("changed local safety policy kept old cache")
	}
	v := base()
	script := v.GenerationContext["recording_script"].(recordingScript)
	script.Tabs[0].Order = 2
	script.Tabs[1].Order = 1
	script.Tabs[0].Title = "欢迎"
	script.Tabs[0].Pinned = true
	script.ActiveTabID = "second"
	script.Tabs[0], script.Tabs[1] = script.Tabs[1], script.Tabs[0]
	v.GenerationContext["recording_script"] = script
	newVoice, newContent := recordingCurrentCacheIdentity(v, recordingStreamTexts(v), policy)
	if newVoice != voice || newContent != content {
		t.Fatal("display-only tab changes discarded valid cache/credit")
	}
	ids := recordingStableTabIDs(v, recordingStreamTexts(v))
	if len(ids) != 2 || ids[0] != "second" || ids[1] != "first" {
		t.Fatalf("tab identity lost after reordering: %v", ids)
	}
}

func TestRecordingLongSentencePreservesCharactersAndPrices(t *testing.T) {
	for _, source := range []string{strings.Repeat("讲", 109) + "337元套餐原价547元", strings.Repeat("讲", 108) + "337.50元原价547元", strings.Repeat("说", 400)} {
		parts := splitRecordingLongSentence(source, 110)
		joined := strings.Join(parts, "")
		if speakableKey(source) != speakableKey(joined) || !sameNumbers(source, joined) {
			t.Fatalf("split changed facts: source=%s joined=%s", source, joined)
		}
	}
}

func TestRecordingApprovedSpokenRequiresPassedReceipt(t *testing.T) {
	if _, err := approvedRecordingSpoken(nil, "欢迎。"); err == nil {
		t.Fatal("nil policy accepted")
	}
	if _, err := approvedRecordingSpoken(&anchorcycle.State{}, "欢迎。"); err == nil {
		t.Fatal("missing report accepted")
	}
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	state := &anchorcycle.State{Surface: "formal_live"}
	if err := state.InstallP0(md, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := approvedRecordingSpoken(state, "欢迎朋友。"); err != nil {
		t.Fatal(err)
	}
	if _, err := approvedRecordingSpoken(state, "这是全国第一，绝对最好。"); err == nil {
		t.Fatal("failed report accepted")
	}
}
