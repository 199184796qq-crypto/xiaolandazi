package httpapi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"livecompanion/management/internal/recordingcache"
)

func reviewIncrementalAssetIDs(entries []recordingcache.Entry) []int64 {
	ids := make([]int64, len(entries))
	for i, entry := range entries {
		ids[i] = entry.AssetID
	}
	return ids
}

func TestRecordingIncrementalReviewDuplicateSourceKeepsParagraphDependency(t *testing.T) {
	old := incrementalSnapshot(t, incrementalVersion("火锅套餐337元。欢迎朋友。\n中餐套餐299元。欢迎朋友。"), 100)
	first, second := 1, 3
	if old.Tabs[0].UnitKeys[first] != old.Tabs[0].UnitKeys[second] || old.Tabs[0].ContextKeys[first] == old.Tabs[0].ContextKeys[second] {
		t.Fatal("fixture must share literal identity while separating paragraph context")
	}
	old.Entries = []recordingcache.Entry{
		incrementalEntry(old.Tabs[0], first, 101, false),
		incrementalEntry(old.Tabs[0], first, 102, true),
		incrementalEntry(old.Tabs[0], second, 103, true),
	}
	unchanged := incrementalSnapshot(t, incrementalVersion("火锅套餐337元。欢迎朋友。\n中餐套餐299元。欢迎朋友。"), 101)
	inheritRecordingCache(old, unchanged, false)
	hit := recordingScopedCacheLookup(unchanged)
	for index, want := range map[int][]int64{first: {101, 102}, second: {101, 103}} {
		got := recordingCacheCandidates(hit, unchanged.Tabs[0].UnitKeys[index], unchanged.Tabs[0].ContextKeys[index])
		if !reflect.DeepEqual(reviewIncrementalAssetIDs(got), want) {
			t.Fatal("same literal sentence reused another paragraph's rewrite", index, reviewIncrementalAssetIDs(got), want)
		}
	}

	changed := incrementalSnapshot(t, incrementalVersion("火锅套餐338元。欢迎朋友。\n中餐套餐299元。欢迎朋友。"), 102)
	inheritRecordingCache(unchanged, changed, false)
	hit = recordingScopedCacheLookup(changed)
	if got := reviewIncrementalAssetIDs(recordingCacheCandidates(hit, changed.Tabs[0].UnitKeys[first], changed.Tabs[0].ContextKeys[first])); !reflect.DeepEqual(got, []int64{101}) {
		t.Fatal("edited paragraph kept its old rewrite or borrowed another paragraph's rewrite", got)
	}
	if got := reviewIncrementalAssetIDs(recordingCacheCandidates(hit, changed.Tabs[0].UnitKeys[second], changed.Tabs[0].ContextKeys[second])); !reflect.DeepEqual(got, []int64{101, 103}) {
		t.Fatal("editing another paragraph discarded this paragraph's valid rewrite", got)
	}
}

func TestRecordingIncrementalReviewDuplicateParagraphVersionsPrunedSeparately(t *testing.T) {
	manifest := incrementalSnapshot(t, incrementalVersion("火锅套餐337元。欢迎朋友。\n中餐套餐299元。欢迎朋友。"), 100)
	for i := range 6 {
		manifest.Entries = append(manifest.Entries,
			incrementalEntry(manifest.Tabs[0], 1, int64(100+i), true),
			incrementalEntry(manifest.Tabs[0], 3, int64(200+i), true))
	}
	manifest.Entries = pruneRecordingCacheEntries(manifest.Entries)
	if len(manifest.Entries) != recordingCacheMaxVersions*2 {
		t.Fatal("versions from one paragraph evicted another paragraph's pool", len(manifest.Entries))
	}
	hit := recordingScopedCacheLookup(manifest)
	for _, index := range []int{1, 3} {
		if got := recordingCacheCandidates(hit, manifest.Tabs[0].UnitKeys[index], manifest.Tabs[0].ContextKeys[index]); len(got) != recordingCacheMaxVersions {
			t.Fatal("paragraph has the wrong number of reusable versions", index, len(got))
		}
	}
}

func TestRecordingIncrementalReviewCrossTabReferenceInvalidatesOnPriceEdit(t *testing.T) {
	version := incrementalVersion("套餐337元。")
	script := version.GenerationContext["recording_script"].(recordingScript)
	script.Tabs = append(script.Tabs,
		recordingScriptTab{TabID: "reference", Text: "刚才那个价格请核对页面。"},
		recordingScriptTab{TabID: "independent", Text: "欢迎新朋友。"})
	version.GenerationContext["recording_script"] = script
	old := incrementalSnapshot(t, version, 100)
	old.Entries = []recordingcache.Entry{
		incrementalEntry(old.Tabs[1], 0, 101, false),
		incrementalEntry(old.Tabs[1], 0, 102, true),
		incrementalEntry(old.Tabs[2], 0, 103, false),
		incrementalEntry(old.Tabs[2], 0, 104, true),
	}
	script.Tabs[0].Text = "套餐338元。"
	version.GenerationContext["recording_script"] = script
	next := incrementalSnapshot(t, version, 101)
	inheritRecordingCache(old, next, false)
	if got := reviewIncrementalAssetIDs(next.Entries); !reflect.DeepEqual(got, []int64{103, 104}) {
		t.Fatal("cross-tab reference survived changed price or unrelated independent text was lost", got)
	}
}

func TestRecordingIncrementalReviewCorruptActiveNeverBootstrapsLegacy(t *testing.T) {
	scope, err := recordingcache.ScopeFor(t.TempDir(), 14, 29, 5)
	if err != nil {
		t.Fatal(err)
	}
	blueprint := incrementalSnapshot(t, incrementalVersion("欢迎朋友。"), 100)
	legacy := *blueprint
	legacy.Version, legacy.Tabs = 2, nil
	legacy.Entries = []recordingcache.Entry{incrementalEntry(blueprint.Tabs[0], 0, 101, false)}
	legacy.Entries[0].UnitKey, legacy.Entries[0].ContextKey = "", ""
	if err := scope.Save(&legacy); err != nil {
		t.Fatal(err)
	}
	activePath := filepath.Join(scope.Root, "recording-cache", "active.json")
	corrupt := []byte("{\"version\":3,\"entries\":[")
	if err := os.WriteFile(activePath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if manifest, _, _, err := openRecordingIncrementalCache(scope, blueprint); err == nil || manifest != nil {
		t.Fatal("damaged active lineage was replaced using historical legacy cache", manifest, err)
	}
	if after, err := os.ReadFile(activePath); err != nil || !reflect.DeepEqual(after, corrupt) {
		t.Fatal("failed reconciliation overwrote the damaged active evidence", err)
	}
}

func TestRecordingIncrementalReviewSnapshotMatchesProducerSourceIndices(t *testing.T) {
	for _, text := range []string{
		"欢迎朋友。\n套餐337元。请提前预约。",
		"欢迎朋友\r\n\r\n套餐337元；\r\n请提前预约",
		"第一行\n第二行！\n\t第三行?\n",
		"一。！？；!?;\n二",
	} {
		t.Run(text, func(t *testing.T) {
			snapshot := incrementalSnapshot(t, incrementalVersion(text), 100)
			if got, want := snapshot.Tabs[0].Sources, splitRecordingSentences(text); !reflect.DeepEqual(got, want) {
				t.Fatal("snapshot's source keys no longer align with producer indices", got, want)
			}
		})
	}
}

func TestRecordingIncrementalReviewLegacyDuplicateSourceIsNotGuessed(t *testing.T) {
	scope, _ := recordingcache.ScopeFor(t.TempDir(), 14, 29, 5)
	blueprint := incrementalSnapshot(t, incrementalVersion("套餐337元。欢迎朋友。\n套餐299元。欢迎朋友。"), 100)
	legacy := *blueprint
	legacy.Version, legacy.Tabs = 2, nil
	legacy.Entries = []recordingcache.Entry{incrementalEntry(blueprint.Tabs[0], 1, 101, true)}
	legacy.Entries[0].UnitKey, legacy.Entries[0].ContextKey = "", ""
	if err := scope.Save(&legacy); err != nil {
		t.Fatal(err)
	}
	manifest, _, stats, err := openRecordingIncrementalCache(scope, blueprint)
	if err != nil || len(manifest.Entries) != 0 || stats.InheritedVersions != 0 {
		t.Fatal("legacy duplicate occurrence was assigned a fabricated paragraph dependency", manifest, stats, err)
	}
}
