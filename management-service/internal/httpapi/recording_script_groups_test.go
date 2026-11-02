package httpapi

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func groupedScriptFixture() recordingScript {
	return recordingScript{Version: 2, Tabs: []recordingScriptTab{
		{TabID: "intro", Title: "开场", Pinned: true, Entries: []recordingScriptEntry{{EntryID: "s1", Text: strings.Repeat("甲", 600)}, {EntryID: "s2", Text: strings.Repeat("乙", 400)}}},
		{TabID: "product", Title: "商品", Order: 1, Entries: []recordingScriptEntry{{EntryID: "s1", Text: strings.Repeat("丙", 500)}}},
	}, ActiveTabID: "product"}
}

func TestRecordingScriptGroupsSaveAndReadBack(t *testing.T) {
	script := groupedScriptFixture()
	// New entries keep exact original whitespace/punctuation and literal ====;
	// those characters no longer define paragraph boundaries.
	script.Tabs[0].Entries[0].Text = "价格 87.6元。\n====\n" + strings.Repeat("甲", 580)
	ctx := map[string]any{"recording_script": script}
	if err := normalizeRecordingScript(ctx); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &ctx); err != nil {
		t.Fatal(err)
	}
	read, ok := recordingScriptFromContext(ctx)
	if !ok || !reflect.DeepEqual(read, script) {
		t.Fatalf("not saved independently: %#v", read)
	}
	if strings.Contains(string(raw), `"text":"","`) {
		t.Fatal("new schema should not synthesize a combined legacy body")
	}
}

func TestRecordingScriptGroupsLimits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lengths []int
		valid   bool
	}{
		{"minimum", []int{400}, true}, {"maximum", []int{4000}, true}, {"short", []int{399}, false}, {"long", []int{4001}, false},
		{"group-boundary", []int{3600, 400}, true}, {"group-overflow", []int{3601, 400}, false}, {"ten", []int{400, 400, 400, 400, 400, 400, 400, 400, 400, 400}, true}, {"empty", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := groupedScriptFixture()
			s.Tabs = s.Tabs[:1]
			s.ActiveTabID = "intro"
			s.Tabs[0].Entries = []recordingScriptEntry{}
			for i, n := range tc.lengths {
				s.Tabs[0].Entries = append(s.Tabs[0].Entries, recordingScriptEntry{EntryID: strings.Repeat("s", i+1), Text: strings.Repeat("。", n)})
			}
			ctx := map[string]any{"recording_script": s}
			err := normalizeRecordingScript(ctx)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
		})
	}
	for _, kind := range []string{"duplicate-id", "missing-id", "mixed-legacy-body", "wrong-version", "blank-only"} {
		t.Run(kind, func(t *testing.T) {
			s := groupedScriptFixture()
			switch kind {
			case "duplicate-id":
				s.Tabs[0].Entries[1].EntryID = "s1"
			case "missing-id":
				s.Tabs[0].Entries[0].EntryID = ""
			case "mixed-legacy-body":
				s.Tabs[0].Text = "旧正文"
			case "wrong-version":
				s.Version = 0
			case "blank-only":
				s.Tabs[0].Entries[0].Text = strings.Repeat(" ", 400)
			}
			if normalizeRecordingScript(map[string]any{"recording_script": s}) == nil {
				t.Fatal("invalid group accepted")
			}
		})
	}
}

func TestRecordingScriptGroupsIndependentSupplyAndShortestWarmup(t *testing.T) {
	for _, modelName := range []string{ttsgateway.VolcengineICL2, "qwen-audio-3.1-tts-flash"} {
		t.Run(modelName, func(t *testing.T) {
			script := groupedScriptFixture()
			script.Tabs[0].Entries[0].Text = "甲====乙" + strings.Repeat("甲", 2500)
			v := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": "stream_clone", "recording_script": script}, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: modelName, VoiceID: "fixture", Rate: 1}}
			if !recordingScriptOnlyVersion(v) {
				t.Fatal("grouped script is not playable")
			}
			texts := recordingStreamTexts(v)
			if len(texts) != 3 || texts[0] != script.Tabs[0].Entries[0].Text {
				t.Fatal("flattening joined or split entries")
			}
			if ids := recordingStableTabIDs(v, texts); !reflect.DeepEqual(ids, []string{"intro:s1", "intro:s2", "product:s1"}) {
				t.Fatal(ids)
			}
			if !reflect.DeepEqual(recordingStreamPinned(v), []bool{true, true, false}) {
				t.Fatal("pin ownership lost")
			}
			units, err := scriptFirstUnits(v, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if len(units) != 3 || units[0].Text != texts[1] || units[1].Text != texts[0] || units[2].Text != texts[2] {
				t.Fatal("shortest first must preserve all other entries")
			}
			for _, unit := range units {
				if filepath.Base(unit.Dir) != scriptFirstParagraphKey(v, unit.Text) {
					t.Fatal("warmup/live cache identity differs")
				}
			}
			if !reflect.DeepEqual(recordingStreamTexts(v), texts) {
				t.Fatal("runtime ordering mutated saved group order")
			}
		})
	}
}
