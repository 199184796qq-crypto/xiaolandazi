package httpapi

import (
	"encoding/json"
	"fmt"
	"strings"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/recordingcache"
)

// recordingEffectiveVersionCount inspects only candidates already admitted by
// the caller's UnitKey/Context and current fact/voice identity. It neither loads
// another manifest nor resolves URLs/assets or invokes a model. A positive
// count proves a structurally usable, currently approved version, not that its
// remote object has been probed or that any audio has played.
func recordingEffectiveVersionCount(source string, memory [][]recordingReadyUnit, cached []recordingcache.Entry, policy *anchorcycle.State) int {
	source = strings.TrimSpace(source)
	if source == "" || policy == nil {
		return 0
	}
	var versions [][]recordingInventoryPart
	for _, units := range memory {
		if parts, ok := recordingInventoryParts(units); ok {
			versions = append(versions, parts)
		}
	}
	for _, entry := range cached {
		if strings.TrimSpace(entry.Sentence) != source {
			continue // A stale source cannot contribute inventory or poison it.
		}
		parts := entry.Parts
		if len(parts) == 0 {
			parts = []recordingcache.Part{{Spoken: entry.Spoken, AssetID: entry.AssetID, Duration: entry.Duration, Segments: entry.Segments}}
		}
		units := make([]recordingReadyUnit, 0, len(parts))
		for _, part := range parts {
			if part.AssetID <= 0 || part.Duration <= 0 || int64(int(part.Duration)) != part.Duration {
				units = nil
				break
			}
			track := coreclient.ProgramRefreshTrack{ID: fmt.Sprintf("recording-cache-%d", part.AssetID), Text: part.Spoken,
				AudioURL: "inventory://structural-check", DurationMS: int(part.Duration)}
			for index, segment := range part.Segments {
				if int64(int(segment.StartMS)) != segment.StartMS || int64(int(segment.EndMS)) != segment.EndMS {
					track.Timeline = nil
					break
				}
				track.Timeline = append(track.Timeline, coreclient.ProgramRefreshTimelineSegment{SegmentID: fmt.Sprintf("inventory-%d", index+1),
					Index: index + 1, Text: segment.Text, StartMS: int(segment.StartMS), EndMS: int(segment.EndMS), SafeCut: segment.SafeCut})
			}
			units = append(units, recordingReadyUnit{Text: part.Spoken, Track: track})
		}
		if parts, ok := recordingInventoryParts(units); ok {
			versions = append(versions, parts)
		}
	}
	// Inspect the complete pool before applying the four-version cap. Otherwise
	// a later conflicting entry could leave an earlier alias falsely counted.
	assetPayload := map[int64]string{}
	conflicts := map[int64]bool{}
	for _, version := range versions {
		for _, part := range version {
			payload := recordingInventoryKey(part)
			if previous, exists := assetPayload[part.AssetID]; exists && previous != payload {
				conflicts[part.AssetID] = true
			}
			assetPayload[part.AssetID] = payload
		}
	}
	seen := map[string]bool{}
	approved := map[string]bool{}
	passes := func(text string) bool {
		if result, exists := approved[text]; exists {
			return result
		}
		spoken, err := approvedRecordingSpoken(policy, text)
		result := err == nil && strings.TrimSpace(spoken) == strings.TrimSpace(text)
		approved[text] = result
		return result
	}
	count := 0
	for _, version := range versions {
		key := recordingInventoryKey(version)
		if seen[key] {
			continue
		}
		seen[key] = true
		valid := true
		var whole strings.Builder
		for _, part := range version {
			if conflicts[part.AssetID] || !passes(part.Spoken) {
				valid = false
				break
			}
			whole.WriteString(part.Spoken)
		}
		if valid && passes(whole.String()) {
			count++
		}
	}
	if count > recordingCacheMaxVersions {
		return recordingCacheMaxVersions
	}
	return count
}

// Runtime IDs, labels, safe-point IDs and URL signatures change when the same
// immutable asset is rebuilt. Its spoken text/duration/timeline do not.
type recordingInventoryPart struct {
	AssetID  int64
	Spoken   string
	Duration int64
	Segments []recordingcache.Segment
}

func recordingInventoryKey(value any) string {
	encoded, _ := json.Marshal(value) // Only the plain scalar/slice types above.
	return string(encoded)
}

func recordingInventoryParts(units []recordingReadyUnit) ([]recordingInventoryPart, bool) {
	if !validRecordingReuseUnits(units) {
		return nil, false
	}
	parts := make([]recordingInventoryPart, 0, len(units))
	seenAssets := map[int64]bool{}
	for _, unit := range units {
		asset := recordingAssetIDFromTrack(unit.Track.ID)
		if asset <= 0 || strings.Contains(unit.Track.ID, "--") || seenAssets[asset] || len(unit.Track.Timeline) > 1024 {
			return nil, false
		}
		seenAssets[asset] = true
		part := recordingInventoryPart{AssetID: asset, Spoken: strings.TrimSpace(unit.Text), Duration: int64(unit.Track.DurationMS)}
		previousEnd := 0
		var transcript strings.Builder
		for index, segment := range unit.Track.Timeline {
			text := strings.TrimSpace(segment.Text)
			if strings.TrimSpace(segment.SegmentID) == "" || segment.Index != index+1 || text == "" || segment.StartMS < previousEnd ||
				segment.StartMS < 0 || segment.EndMS <= segment.StartMS || (segment.EndMS > unit.Track.DurationMS && segment.EndMS-unit.Track.DurationMS > 5) {
				return nil, false
			}
			part.Segments = append(part.Segments, recordingcache.Segment{Text: text, StartMS: int64(segment.StartMS), EndMS: int64(segment.EndMS), SafeCut: segment.SafeCut})
			transcript.WriteString(text)
			previousEnd = segment.EndMS
		}
		last := part.Segments[len(part.Segments)-1]
		if transcript.String() != part.Spoken || !last.SafeCut || !recordingInventorySemanticEnd(last.Text) {
			return nil, false
		}
		parts = append(parts, part)
	}
	return parts, true
}

func recordingInventorySemanticEnd(text string) bool {
	runes := []rune(strings.TrimSpace(text))
	return len(runes) > 0 && strings.ContainsRune("。！？!?；;", runes[len(runes)-1])
}
