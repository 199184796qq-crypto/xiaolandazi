package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/recordingcache"
)

func incrementalPolicy(t *testing.T) *anchorcycle.State {
	t.Helper()
	md, _, err := loadP0Mother()
	if err != nil {
		t.Fatal(err)
	}
	state := &anchorcycle.State{Surface: "formal_live"}
	if err := state.InstallP0(md, "incremental-local-test"); err != nil {
		t.Fatal(err)
	}
	return state
}

func incrementalVersion(text string) model.LiveAgentPlanVersion {
	return model.LiveAgentPlanVersion{ID: 100, PlanID: 5,
		VoiceIdentity: model.LiveAgentVoiceIdentity{Source: "official", VoiceID: "fixture-voice", Model: "fixture-tts", Rate: 1},
		GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": "ai_stream",
			"facts": "套餐以当前已保存事实为准", "recording_script": recordingScript{Tabs: []recordingScriptTab{{TabID: "intro", Text: text}}},
			"recording_original_sources": []any{map[string]any{"transcript": text, "compact_bundle": "source-shadow"}}}}
}

func incrementalSnapshot(t *testing.T, version model.LiveAgentPlanVersion, session int64) *recordingcache.Manifest {
	t.Helper()
	manifest, err := recordingCacheSnapshot(version, recordingStreamTexts(version), incrementalPolicy(t), session)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func incrementalEntry(tab recordingcache.SourceTab, index int, id int64, polished bool) recordingcache.Entry {
	source := tab.Sources[index]
	spoken := source
	if polished {
		spoken = "朋友，" + source
	}
	return recordingcache.Entry{Sentence: source, UnitKey: tab.UnitKeys[index], ContextKey: tab.ContextKeys[index],
		Spoken: spoken, AssetID: id, Duration: 1000, Polished: polished,
		Segments: []recordingcache.Segment{{Text: spoken, EndMS: 1000, SafeCut: true}}}
}

func TestRecordingIncrementalChangedSourceDropsAllOldVersions(t *testing.T) {
	old := incrementalSnapshot(t, incrementalVersion("欢迎朋友。\n套餐337元。请提前预约。"), 101)
	for i := range old.Tabs[0].Sources {
		old.Entries = append(old.Entries, incrementalEntry(old.Tabs[0], i, int64(100+i), false), incrementalEntry(old.Tabs[0], i, int64(200+i), true))
	}
	next := incrementalSnapshot(t, incrementalVersion("欢迎朋友。\n套餐338元。请提前预约。"), 102)
	if old.CompatibilityKey != next.CompatibilityKey || old.ScriptHash == next.ScriptHash {
		t.Fatal("source content and stable dependencies were not separated")
	}
	_, stats := inheritRecordingCache(old, next, false)
	if stats.InheritedVersions != 3 || stats.InvalidVersions != 3 {
		t.Fatalf("want 2 welcome versions and the literal appointment, got %+v %+v", stats, next.Entries)
	}
	for _, entry := range next.Entries {
		if strings.Contains(entry.Spoken, "337") || (entry.Sentence == "请提前预约。" && entry.Polished) {
			t.Fatal("changed source or old paragraph rewrite survived", entry)
		}
	}
	// A large edit ratio does not discard unrelated valid literal audio.
	large := incrementalSnapshot(t, incrementalVersion("欢迎朋友。\n已换成全新的一篇长文。"), 103)
	inheritRecordingCache(next, large, false)
	if len(large.Entries) != 2 {
		t.Fatal("large update discarded an untouched paragraph", large.Entries)
	}
}

func TestRecordingIncrementalContextAndTabIsolation(t *testing.T) {
	v := incrementalVersion("套餐337元。\n这个价格请按页面核对。")
	script := v.GenerationContext["recording_script"].(recordingScript)
	script.Tabs = append(script.Tabs, recordingScriptTab{TabID: "other", Text: "欢迎朋友。"})
	v.GenerationContext["recording_script"] = script
	old := incrementalSnapshot(t, v, 101)
	old.Entries = []recordingcache.Entry{incrementalEntry(old.Tabs[0], 1, 101, false), incrementalEntry(old.Tabs[1], 0, 102, true)}
	script.Tabs[0].Text = "套餐338元。\n这个价格请按页面核对。"
	script.Tabs[1].Title, script.Tabs[1].Pinned = "新标题", true
	script.Tabs[0], script.Tabs[1] = script.Tabs[1], script.Tabs[0]
	v.GenerationContext["recording_script"] = script
	next := incrementalSnapshot(t, v, 102)
	inheritRecordingCache(old, next, false)
	if len(next.Entries) != 1 || next.Entries[0].AssetID != 102 {
		t.Fatal("cross-tab reference retained an old source dependency or display order lost unrelated audio", next.Entries)
	}
	// Identical words in different stable tabs do not share the same pool.
	a := incrementalEntry(next.Tabs[0], 0, 301, false)
	b := a
	b.UnitKey = recordingcache.VoiceKey("other-tab", a.Sentence)
	b.AssetID = 302
	entries := []recordingcache.Entry{}
	for range 5 {
		entries = append(entries, a, b)
	}
	if got := pruneRecordingCacheEntries(entries); len(got) != 8 {
		t.Fatal("per-tab identical source versions were incorrectly pooled", len(got))
	}
}

func TestRecordingIncrementalChangedDependencyNeverInherits(t *testing.T) {
	old := incrementalSnapshot(t, incrementalVersion("欢迎朋友。"), 100)
	old.Entries = []recordingcache.Entry{incrementalEntry(old.Tabs[0], 0, 101, false)}
	for _, field := range []string{"facts", "unknown", "voice", "rate", "scope", "allowance", "policy", "emotion"} {
		t.Run(field, func(t *testing.T) {
			v, policy := incrementalVersion("欢迎朋友。"), incrementalPolicy(t)
			switch field {
			case "facts":
				v.GenerationContext["facts"] = "套餐价格已变"
			case "unknown":
				v.GenerationContext["custom_rules"] = map[string]any{"text": "不得承诺到货"}
			case "voice":
				v.VoiceIdentity.VoiceID = "voice-new"
			case "rate":
				v.VoiceIdentity.Rate = .9
			case "scope":
				policy.Config.ComplianceScope = "changed"
			case "allowance":
				policy.Config.SkipReview = true
			case "policy":
				policy.P0PolicyMD += "\n新规则"
			case "emotion":
				enabled := true
				v.VoiceIdentity.EmotionEnabled = &enabled
			}
			next, err := recordingCacheSnapshot(v, recordingStreamTexts(v), policy, 101)
			if err != nil {
				t.Fatal(err)
			}
			inheritRecordingCache(old, next, false)
			if len(next.Entries) != 0 {
				t.Fatal("dependency change relabelled old audio", next.Entries)
			}
		})
	}
}

func TestRecordingIncrementalRewriteMotherAndLiteralMetadataAreVerified(t *testing.T) {
	version := incrementalVersion("欢迎朋友。套餐337元。")
	old := incrementalSnapshot(t, version, 100)
	entry := incrementalEntry(old.Tabs[0], 0, 101, false)
	// Simulate historical metadata that calls a rewritten sentence "original".
	entry.Spoken = "套餐337元，欢迎朋友。"
	entry.Segments[0].Text = entry.Spoken
	old.Entries = []recordingcache.Entry{entry}
	next := incrementalSnapshot(t, incrementalVersion("欢迎朋友。套餐338元。"), 101)
	inheritRecordingCache(old, next, false)
	if len(next.Entries) != 0 {
		t.Fatal("incorrect literal flag bypassed paragraph dependency", next.Entries)
	}
	// A character outside basic Han must not disappear from literal comparison.
	if recordingCacheEntryIsLiteral(recordingcache.Entry{Sentence: "欢迎𠮷。", Spoken: "欢迎。"}) {
		t.Fatal("non-basic Han was erased while validating a literal cache entry")
	}
	mother, _, err := loadFactSpeechRewritePrompt()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "rewrite.md")
	if err := os.WriteFile(path, []byte(mother+"\n新增表达要求。"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FACT_SPEECH_REWRITE_PROMPT_PATH", path)
	changed := incrementalSnapshot(t, version, 102)
	if changed.CompatibilityKey == old.CompatibilityKey {
		t.Fatal("actual rewrite mother changed without invalidating its dependency identity")
	}
}

func TestRecordingIncrementalNoResurrectionOnRevertOrPartialGeneration(t *testing.T) {
	scope, _ := recordingcache.ScopeFor(t.TempDir(), 14, 29, 5)
	first := incrementalSnapshot(t, incrementalVersion("旧套餐337元。欢迎朋友。"), 100)
	first.Entries = []recordingcache.Entry{incrementalEntry(first.Tabs[0], 0, 101, false), incrementalEntry(first.Tabs[0], 0, 102, true), incrementalEntry(first.Tabs[0], 1, 103, false)}
	if ok, err := scope.ClaimActive("", first); err != nil || !ok {
		t.Fatal(ok, err)
	}
	// A historical exact namespace must never be a fallback after active exists.
	legacy := *first
	legacy.Version = 2
	if err := scope.Save(&legacy); err != nil {
		t.Fatal(err)
	}
	changed, _, _, err := openRecordingIncrementalCache(scope, incrementalSnapshot(t, incrementalVersion("新套餐338元。欢迎朋友。"), 101))
	if err != nil || len(changed.Entries) != 1 {
		t.Fatal(changed, err)
	}
	// No successful replacement yet: the old versions are already unavailable.
	reverted, _, _, err := openRecordingIncrementalCache(scope, incrementalSnapshot(t, incrementalVersion("旧套餐337元。欢迎朋友。"), 102))
	if err != nil || len(reverted.Entries) != 1 || reverted.Entries[0].AssetID != 103 {
		t.Fatal("reverting source resurrected discarded old versions", reverted, err)
	}
	if ok, err := scope.SaveActive(first); err != nil || ok {
		t.Fatal("late old writer restored discarded cache", ok, err)
	}
	if _, _, _, err := openRecordingIncrementalCache(scope, first); err != errRecordingCacheSuperseded {
		t.Fatal("older session reclaimed ownership", err)
	}
	deleted, _, _, err := openRecordingIncrementalCache(scope, incrementalSnapshot(t, incrementalVersion("欢迎朋友。"), 103))
	if err != nil || len(deleted.Entries) != 1 {
		t.Fatal("deleted source remains available", deleted, err)
	}
	// Selectively clearing the latest namespace must not erase the fact that
	// V2 bootstrap has already ended, while historical A still exists on disk.
	if err := scope.RemoveCache(deleted.VoiceKey, deleted.ScriptHash); err != nil {
		t.Fatal(err)
	}
	afterClear, _, stats, err := openRecordingIncrementalCache(scope, incrementalSnapshot(t, incrementalVersion("旧套餐337元。欢迎朋友。"), 104))
	if err != nil || len(afterClear.Entries) != 0 || stats.LegacyExact {
		t.Fatal("selective clear reopened an invalidated historical V2 namespace", afterClear, stats, err)
	}
}

func TestRecordingIncrementalLegacyOnlyExactBootstrap(t *testing.T) {
	for _, edited := range []bool{false, true} {
		t.Run(fmt.Sprint(edited), func(t *testing.T) {
			scope, _ := recordingcache.ScopeFor(t.TempDir(), 14, 29, 5)
			old := incrementalSnapshot(t, incrementalVersion("欢迎朋友。"), 100)
			old.Version = 2
			old.Entries = []recordingcache.Entry{incrementalEntry(old.Tabs[0], 0, 101, false)}
			old.Entries[0].UnitKey, old.Entries[0].ContextKey = "", ""
			old.Tabs = nil
			if err := scope.Save(old); err != nil {
				t.Fatal(err)
			}
			v := incrementalVersion("欢迎朋友。")
			if edited {
				v = incrementalVersion("欢迎朋友。请看商品。")
			}
			next, _, stats, err := openRecordingIncrementalCache(scope, incrementalSnapshot(t, v, 101))
			if err != nil || (len(next.Entries) == 1) == edited || stats.LegacyExact == edited {
				t.Fatal("unauthenticated legacy migration", next, stats, err)
			}
		})
	}
	ids := func(text string) []string {
		return recordingStableTabIDs(model.LiveAgentPlanVersion{}, []string{text})
	}
	if !reflect.DeepEqual(ids("旧文案。"), ids("新文案。")) {
		t.Fatal("legacy single-tab identity still depends on its full body")
	}
}

func TestRecordingIncrementalEditedCrossProcessOnlyPaysForChangedSource(t *testing.T) {
	root := t.TempDir()
	for _, phase := range []string{"write", "read"} {
		command := exec.Command(os.Args[0], "-test.run=^TestRecordingIncrementalProcessChild$", "-test.count=1", "-test.v")
		command.Env = append(os.Environ(), "RECORDING_INCREMENTAL_PHASE="+phase, "RECORDING_INCREMENTAL_ROOT="+root)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s child: %v\n%s", phase, err, output)
		}
	}
	var evidence struct {
		PID, TTSCalls, ModelCalls int
		Assets                    []int64
	}
	raw, err := os.ReadFile(filepath.Join(root, "read.json"))
	if err != nil || json.Unmarshal(raw, &evidence) != nil || evidence.PID == os.Getpid() || evidence.TTSCalls != 1 || evidence.ModelCalls != 0 || !reflect.DeepEqual(evidence.Assets, []int64{100, 201, 102}) {
		t.Fatal("edited restart charged unchanged sources", evidence, err)
	}
	t.Log("fresh process: unchanged original sources reused with zero model/TTS; changed price source generated exactly once using a local paid-boundary stub")
}

func TestRecordingIncrementalProcessChild(t *testing.T) {
	phase := os.Getenv("RECORDING_INCREMENTAL_PHASE")
	if phase == "" {
		t.Skip("independent process fixture only")
	}
	root := os.Getenv("RECORDING_INCREMENTAL_ROOT")
	scope, err := recordingcache.ScopeFor(root, 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	text, session := "欢迎朋友。套餐337元。请提前预约。", int64(100)
	if phase == "read" {
		text, session = "欢迎朋友。套餐338元。请提前预约。", 101
	}
	manifest, _, _, err := openRecordingIncrementalCache(scope, incrementalSnapshot(t, incrementalVersion(text), session))
	if err != nil {
		t.Fatal(err)
	}
	evidence := struct {
		PID, TTSCalls, ModelCalls int
		Assets                    []int64
	}{PID: os.Getpid()}
	hit := recordingScopedCacheLookup(manifest)
	for index, source := range manifest.Tabs[0].Sources {
		key := manifest.Tabs[0].UnitKeys[index]
		result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
			Cached: hit[key],
			BuildCache: func(_ context.Context, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
				if _, err := approvedRecordingSpoken(incrementalPolicy(t), entry.Spoken); err != nil {
					return nil, false
				}
				if bytes, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("audio-%d.wav", entry.AssetID))); err != nil || string(bytes) != entry.Spoken {
					return nil, false
				}
				return []recordingReadyUnit{recordingReuseFixtureUnit(fmt.Sprintf("recording-%d-block-%d", session, entry.AssetID), entry.Spoken)}, true
			},
			Generate: func(context.Context) ([]recordingReadyUnit, error) {
				if _, err := approvedRecordingSpoken(incrementalPolicy(t), source); err != nil {
					return nil, err
				}
				evidence.TTSCalls++
				id := int64(100 + index)
				if phase == "read" {
					id = 201
				}
				if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("audio-%d.wav", id)), []byte(source), 0o600); err != nil {
					return nil, err
				}
				return []recordingReadyUnit{recordingReuseFixtureUnit(fmt.Sprintf("recording-%d-block-%d", session, id), source)}, nil
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		id := recordingAssetIDFromTrack(result.Units[0].Track.ID)
		evidence.Assets = append(evidence.Assets, id)
		if result.Generated {
			entry, ok := recordingCacheEntryFromUnits(source, result.Units)
			if !ok {
				t.Fatal("new local fixture cannot persist")
			}
			entry.UnitKey, entry.ContextKey = key, manifest.Tabs[0].ContextKeys[index]
			manifest.Entries = append(manifest.Entries, entry)
		}
	}
	if ok, err := scope.SaveActive(manifest); err != nil || !ok {
		t.Fatal(ok, err)
	}
	raw, _ := json.Marshal(evidence)
	if err := os.WriteFile(filepath.Join(root, phase+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
