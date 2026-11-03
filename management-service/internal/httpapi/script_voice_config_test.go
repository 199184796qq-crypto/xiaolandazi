package httpapi

import (
	"testing"

	"livecompanion/management/internal/model"
)

func TestRecordingScriptPersistsVoicesAndMode(t *testing.T) {
	context := map[string]any{"recording_script": map[string]any{
		"version":         2,
		"play_mode":       "single",
		"version_cap":     6,
		"min_voice_block": 4,
		"tabs": []any{map[string]any{
			"tab_id": "t1",
			"title":  "开场",
			"entries": []any{
				map[string]any{"entry_id": "a", "text": longText("第一句")},
			},
		}},
		"voices": []any{
			map[string]any{"voice_id": "voice-a", "model": "qwen-audio-3.1-tts-flash"},
			map[string]any{"voice_id": "voice-b", "model": "seed-tts-2.0-standard"},
			map[string]any{"voice_id": "voice-a", "model": "qwen-audio-3.1-tts-flash"},
			map[string]any{"voice_id": "  ", "model": "x"},
		},
	}}
	if err := normalizeRecordingScript(context); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	script := context["recording_script"].(recordingScript)
	if got := script.EffectivePlayMode(); got != "single" {
		t.Fatalf("播放模式应为 single，实际 %s", got)
	}
	if script.EffectiveVersionCap() != 6 || script.EffectiveMinVoiceBlock() != 4 {
		t.Fatalf("配置未保留: cap=%d block=%d", script.EffectiveVersionCap(), script.EffectiveMinVoiceBlock())
	}
	voices := script.EffectiveVoices()
	if len(voices) != 2 {
		t.Fatalf("重复和空的音色应被去掉，实际 %d: %+v", len(voices), voices)
	}
	if voices[0].VoiceID != "voice-a" || voices[0].Model != "qwen-audio-3.1-tts-flash" {
		t.Fatalf("音色顺序或模型不对: %+v", voices[0])
	}
	if voices[1].VoiceID != "voice-b" {
		t.Fatalf("第二个音色不对: %+v", voices[1])
	}
}

func TestRecordingScriptDefaultsWhenUnset(t *testing.T) {
	script := recordingScript{Version: 2}
	script.normalize()
	if script.EffectivePlayMode() != "full" {
		t.Fatal("未配置时应默认全读")
	}
	if script.EffectiveVersionCap() != 6 || script.EffectiveMinVoiceBlock() != 4 {
		t.Fatal("未配置时应用默认值 6 / 4")
	}
	if len(script.EffectiveVoices()) != 0 {
		t.Fatal("未配置音色时应为空")
	}
}

func TestScriptVoicesForPlaybackFallsBackToPlanVoice(t *testing.T) {
	version := model.LiveAgentPlanVersion{GenerationContext: map[string]any{}}
	version.VoiceIdentity.VoiceID = "plan-voice"
	version.VoiceIdentity.Model = "qwen"
	voices := scriptVoicesForPlayback(version)
	if len(voices) != 1 || voices[0].VoiceID != "plan-voice" || voices[0].Model != "qwen" {
		t.Fatalf("应退化为方案当前音色: %+v", voices)
	}
	// 配置了多音色时按配置走。
	version.GenerationContext["recording_script"] = recordingScript{
		Version: 2,
		Tabs:    []recordingScriptTab{{TabID: "t1", Entries: []recordingScriptEntry{{EntryID: "a", Text: longText("x")}}}},
		Voices:  []recordingScriptVoice{{VoiceID: "a", Model: "m1"}, {VoiceID: "b", Model: "m2"}},
	}
	voices = scriptVoicesForPlayback(version)
	if len(voices) != 3 || voices[0].VoiceID != "plan-voice" {
		t.Fatalf("应保留已授权正式音色再返回配置音色: %+v", voices)
	}
}
