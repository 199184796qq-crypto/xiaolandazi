package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/recordingcache"
)

func inventoryFixtureUnit(id int64, text string) recordingReadyUnit {
	return recordingReadyUnit{Text: text, Track: coreclient.ProgramRefreshTrack{
		ID: fmt.Sprintf("recording-1-0-%d", id), Text: text, AudioURL: "fixture://audio", DurationMS: 1000,
		Timeline: []coreclient.ProgramRefreshTimelineSegment{{SegmentID: "generated-1", Index: 1, Text: text, EndMS: 1000, SafeCut: true}},
	}}
}

func inventoryFixtureEntry(t *testing.T, source string, units ...recordingReadyUnit) recordingcache.Entry {
	t.Helper()
	entry, ok := recordingCacheEntryFromUnits(source, units)
	if !ok {
		t.Fatal("fixture could not be represented in the persistent cache")
	}
	return entry
}

func TestRecordingEffectiveVersionCountDeduplicatesRebuiltAudio(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友。"
	generated := inventoryFixtureUnit(11, source)
	rebuilt := inventoryFixtureUnit(11, source)
	rebuilt.Track.ID = "recording-cache-11"
	rebuilt.Track.Label = "recreated label"
	rebuilt.Track.AudioURL = "fixture://renewed-signature"
	rebuilt.Track.Timeline[0].SegmentID = "cache-11-001"
	entry := inventoryFixtureEntry(t, source, generated)
	entry.Hits, entry.UpdatedAt = 10, "2026-10-25T00:00:00Z"
	memory := [][]recordingReadyUnit{{generated}, {rebuilt}}
	cached := []recordingcache.Entry{entry, entry}
	before, _ := json.Marshal(struct {
		Memory [][]recordingReadyUnit
		Cached []recordingcache.Entry
	}{memory, cached})
	if count := recordingEffectiveVersionCount(source, memory, cached, policy); count != 1 {
		t.Fatalf("one immutable audio version counted %d times", count)
	}
	after, _ := json.Marshal(struct {
		Memory [][]recordingReadyUnit
		Cached []recordingcache.Entry
	}{memory, cached})
	if string(before) != string(after) {
		t.Fatal("inventory inspection changed the caller's memory/cache candidates")
	}
}

func TestRecordingEffectiveVersionCountMultipartIsOneVersion(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友，先看看介绍。"
	first := []recordingReadyUnit{inventoryFixtureUnit(21, "欢迎朋友。"), inventoryFixtureUnit(22, "先看看介绍。")}
	second := []recordingReadyUnit{inventoryFixtureUnit(23, "朋友们好。"), inventoryFixtureUnit(24, "一起看看介绍。")}
	entries := []recordingcache.Entry{inventoryFixtureEntry(t, source, first...), inventoryFixtureEntry(t, source, second...)}
	if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{first, second}, entries, policy); count != 2 {
		t.Fatalf("two multipart versions counted as %d", count)
	}
	broken := append([]recordingReadyUnit(nil), first...)
	broken[1].Track.AudioURL = ""
	if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{broken}, nil, policy); count != 0 {
		t.Fatalf("incomplete multipart candidate counted %d", count)
	}
}

func TestRecordingEffectiveVersionCountQuarantinesAssetConflicts(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友。"
	base := inventoryFixtureUnit(31, source)
	otherText := inventoryFixtureUnit(31, "朋友们好。")
	otherTimeline := inventoryFixtureUnit(31, source)
	otherTimeline.Track.Timeline[0].EndMS = 900
	independent := inventoryFixtureUnit(32, "先看看介绍。")
	for name, bad := range map[string]recordingReadyUnit{"different text": otherText, "different timeline": otherTimeline} {
		t.Run(name, func(t *testing.T) {
			entry := inventoryFixtureEntry(t, source, bad)
			if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{{base}, {independent}}, []recordingcache.Entry{entry}, policy); count != 1 {
				t.Fatalf("memory/persistent conflict was counted: %d", count)
			}
			if count := recordingEffectiveVersionCount(source, nil, []recordingcache.Entry{inventoryFixtureEntry(t, source, base), entry}, policy); count != 0 {
				t.Fatalf("persistent conflict was counted: %d", count)
			}
		})
	}
	// Conflicts after the capacity boundary must still invalidate early entries.
	memory := [][]recordingReadyUnit{{base}, {independent}, {inventoryFixtureUnit(33, source)}, {inventoryFixtureUnit(34, source)}}
	if count := recordingEffectiveVersionCount(source, memory, []recordingcache.Entry{inventoryFixtureEntry(t, source, otherText)}, policy); count != 3 {
		t.Fatalf("late conflict hidden by four-version cap: %d", count)
	}
}

func TestRecordingEffectiveVersionCountRequiresCurrentApprovalAndSource(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友。"
	valid := inventoryFixtureUnit(41, source)
	bad := inventoryFixtureUnit(42, "这是全国第一，绝对最好。")
	if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{{bad}}, []recordingcache.Entry{inventoryFixtureEntry(t, source, bad)}, policy); count != 0 {
		t.Fatalf("current P0 rejection contributed inventory: %d", count)
	}
	for name, state := range map[string]*anchorcycle.State{"nil": nil, "no installed report": {}} {
		t.Run(name, func(t *testing.T) {
			if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{{valid}}, nil, state); count != 0 {
				t.Fatalf("missing current approval contributed inventory: %d", count)
			}
		})
	}
	stale := inventoryFixtureEntry(t, "另一句源文。", inventoryFixtureUnit(41, "另一份正文。"))
	if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{{valid}}, []recordingcache.Entry{stale}, policy); count != 1 {
		t.Fatalf("stale source contributed or poisoned current inventory: %d", count)
	}
	if count := recordingEffectiveVersionCount(" ", [][]recordingReadyUnit{{valid}}, nil, policy); count != 0 {
		t.Fatalf("unbound source contributed inventory: %d", count)
	}
}

func TestRecordingEffectiveVersionCountRejectsMalformedReadyUnits(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友。"
	for name, change := range map[string]func(*recordingReadyUnit){
		"no asset":           func(u *recordingReadyUnit) { u.Track.ID = "not-an-asset" },
		"negative asset":     func(u *recordingReadyUnit) { u.Track.ID = "recording-cache--51" },
		"empty audio":        func(u *recordingReadyUnit) { u.Track.AudioURL = "" },
		"text mismatch":      func(u *recordingReadyUnit) { u.Track.Text = "不同正文。" },
		"zero duration":      func(u *recordingReadyUnit) { u.Track.DurationMS = 0 },
		"no timeline":        func(u *recordingReadyUnit) { u.Track.Timeline = nil },
		"no segment id":      func(u *recordingReadyUnit) { u.Track.Timeline[0].SegmentID = "" },
		"out of sequence":    func(u *recordingReadyUnit) { u.Track.Timeline[0].Index = 2 },
		"negative range":     func(u *recordingReadyUnit) { u.Track.Timeline[0].StartMS = -1 },
		"zero range":         func(u *recordingReadyUnit) { u.Track.Timeline[0].EndMS = 0 },
		"beyond audio":       func(u *recordingReadyUnit) { u.Track.Timeline[0].EndMS = 1006 },
		"subtitle mismatch":  func(u *recordingReadyUnit) { u.Track.Timeline[0].Text = "不同字幕。" },
		"no usable safe end": func(u *recordingReadyUnit) { u.Track.Timeline[0].SafeCut = false },
		"false semantic end": func(u *recordingReadyUnit) {
			u.Text = "欢迎朋友，"
			u.Track.Text = u.Text
			u.Track.Timeline[0].Text = u.Text
		},
		"overlapping ranges": func(u *recordingReadyUnit) {
			u.Track.Timeline = append(u.Track.Timeline, coreclient.ProgramRefreshTimelineSegment{SegmentID: "second", Index: 2, StartMS: 900, EndMS: 1000, Text: source, SafeCut: true})
		},
	} {
		t.Run(name, func(t *testing.T) {
			unit := inventoryFixtureUnit(51, source)
			change(&unit)
			if count := recordingEffectiveVersionCount(source, [][]recordingReadyUnit{{unit}}, nil, policy); count != 0 {
				t.Fatalf("malformed candidate contributed inventory: %d", count)
			}
		})
	}
}

func TestRecordingEffectiveVersionCountRejectsMalformedPersistentParts(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友。"
	for name, change := range map[string]func(*recordingcache.Entry){
		"missing asset":  func(e *recordingcache.Entry) { e.AssetID = 0 },
		"negative asset": func(e *recordingcache.Entry) { e.AssetID = -61 },
		"missing speech": func(e *recordingcache.Entry) { e.Spoken = "" },
		"no timeline":    func(e *recordingcache.Entry) { e.Segments = nil },
		"bad range":      func(e *recordingcache.Entry) { e.Segments[0].EndMS = -1 },
		"bad subtitles":  func(e *recordingcache.Entry) { e.Segments[0].Text = "别的字幕。" },
	} {
		t.Run(name, func(t *testing.T) {
			entry := inventoryFixtureEntry(t, source, inventoryFixtureUnit(61, source))
			change(&entry)
			if count := recordingEffectiveVersionCount(source, nil, []recordingcache.Entry{entry}, policy); count != 0 {
				t.Fatalf("malformed persistent candidate contributed inventory: %d", count)
			}
		})
	}
	first := inventoryFixtureUnit(62, source)
	entry := inventoryFixtureEntry(t, source, first, inventoryFixtureUnit(63, "先看看介绍。"))
	entry.Parts[1].AssetID = 0
	if count := recordingEffectiveVersionCount(source, nil, []recordingcache.Entry{entry}, policy); count != 0 {
		t.Fatalf("partially invalid persisted version counted: %d", count)
	}
}

func TestRecordingEffectiveVersionCountIsCappedAfterDeduplication(t *testing.T) {
	policy := incrementalPolicy(t)
	const source = "欢迎朋友。"
	var memory [][]recordingReadyUnit
	var cached []recordingcache.Entry
	for id := int64(70); id < 76; id++ {
		unit := inventoryFixtureUnit(id, strings.TrimSpace(source))
		memory = append(memory, []recordingReadyUnit{unit})
		cached = append(cached, inventoryFixtureEntry(t, source, unit))
	}
	if count := recordingEffectiveVersionCount(source, memory, cached, policy); count != recordingCacheMaxVersions {
		t.Fatalf("inventory exceeded capacity or double-counted candidates: %d", count)
	}
}
