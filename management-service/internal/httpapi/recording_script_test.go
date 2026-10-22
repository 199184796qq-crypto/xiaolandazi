package httpapi

import (
	"strings"
	"testing"
)

func TestNormalizeRecordingScriptAcceptsSingleTab(t *testing.T) {
	context := map[string]any{"recording_script": map[string]any{
		"tabs": []any{map[string]any{"title": "  开场 ", "text": " 你好 "}},
	}}
	if err := normalizeRecordingScript(context); err != nil {
		t.Fatalf("single tab must be accepted: %v", err)
	}
	script, ok := recordingScriptFromContext(context)
	if !ok {
		t.Fatal("normalized script must read back")
	}
	if len(script.Tabs) != 1 || script.Tabs[0].TabID != "t1" || script.Tabs[0].Title != "开场" || script.Tabs[0].Text != "你好" {
		t.Fatalf("unexpected normalized tab: %#v", script.Tabs)
	}
	if script.ActiveTabID != "t1" {
		t.Fatalf("active tab must default to the first tab, got %q", script.ActiveTabID)
	}
}

func TestNormalizeRecordingScriptKeepsLegacyBehaviourWhenAbsent(t *testing.T) {
	context := map[string]any{"content_mode": "user_audio"}
	if err := normalizeRecordingScript(context); err != nil {
		t.Fatalf("absent script must be ignored: %v", err)
	}
	if _, exists := context["recording_script"]; exists {
		t.Fatal("absent script must not be created")
	}
	if _, ok := recordingScriptFromContext(context); ok {
		t.Fatal("absent script must not read back")
	}
}

func TestNormalizeRecordingScriptRejectsBadGroups(t *testing.T) {
	cases := map[string]map[string]any{
		"empty tabs":     {"tabs": []any{}},
		"duplicate id":   {"tabs": []any{map[string]any{"tab_id": "t1"}, map[string]any{"tab_id": "t1"}}},
		"bad id":         {"tabs": []any{map[string]any{"tab_id": "标签 1"}}},
		"active missing": {"tabs": []any{map[string]any{"tab_id": "t1"}}, "active_tab_id": "t9"},
		"order short":    {"tabs": []any{map[string]any{"tab_id": "t1"}, map[string]any{"tab_id": "t2"}}, "playback_order": []any{"t1"}},
		"order unknown":  {"tabs": []any{map[string]any{"tab_id": "t1"}, map[string]any{"tab_id": "t2"}}, "playback_order": []any{"t1", "t3"}},
		"order duplicate": {"tabs": []any{map[string]any{"tab_id": "t1"}, map[string]any{"tab_id": "t2"}},
			"playback_order": []any{"t1", "t1"}},
		"two audios":    {"tabs": []any{map[string]any{"tab_id": "t1", "audio_asset_ids": []any{5, 6}}}},
		"bad audio":     {"tabs": []any{map[string]any{"tab_id": "t1", "audio_asset_ids": []any{0}}}},
		"unknown field": {"tabs": []any{map[string]any{"tab_id": "t1"}}, "loop": true},
	}
	for name, value := range cases {
		context := map[string]any{"recording_script": value}
		if err := normalizeRecordingScript(context); err == nil {
			t.Fatalf("%s: expected rejection", name)
		}
	}
}

func TestNormalizeRecordingScriptAcceptsFullOrder(t *testing.T) {
	context := map[string]any{"recording_script": map[string]any{
		"tabs": []any{
			map[string]any{"tab_id": "intro", "title": "开场", "text": "第一段"},
			map[string]any{"tab_id": "bed", "title": "XX床介绍", "text": "第二段", "audio_asset_ids": []any{12}},
			map[string]any{"tab_id": "set", "title": "XX套餐介绍", "text": "第三段"},
		},
		"active_tab_id":  "set",
		"playback_order": []any{"bed", "set", "intro"},
	}}
	if err := normalizeRecordingScript(context); err != nil {
		t.Fatalf("full group must be accepted: %v", err)
	}
	script, ok := recordingScriptFromContext(context)
	if !ok {
		t.Fatal("script must read back")
	}
	if script.ActiveTabID != "set" || len(script.PlaybackOrder) != 3 || script.PlaybackOrder[0] != "bed" {
		t.Fatalf("order/active not preserved: %#v", script)
	}
	// 播放顺序决定拼接顺序，空文本框跳过。
	if got := script.scriptedText(); got != "第二段\n第三段\n第一段" {
		t.Fatalf("scripted text must follow playback order, got %q", got)
	}
}

func TestRecordingScriptSkipsBlankTabsWhenScripting(t *testing.T) {
	script := recordingScript{Tabs: []recordingScriptTab{
		{TabID: "t1", Text: "甲"},
		{TabID: "t2", Text: "   "},
		{TabID: "t3", Text: "乙"},
	}}
	script.normalize()
	got := script.scriptedText()
	if got != "甲\n乙" {
		t.Fatalf("blank tabs must be skipped, got %q", got)
	}
	if strings.Contains(got, "\n\n") {
		t.Fatal("no empty lines expected")
	}
}
