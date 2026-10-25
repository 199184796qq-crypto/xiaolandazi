package httpapi

import (
	"context"
	"errors"
	"math/rand"
	"reflect"
	"strings"

	"livecompanion/management/internal/recordingcache"
)

// recordingReuseRequest contains only candidates already admitted for the
// current facts, voice and ownership scope. It never searches another scope or
// a legacy key. Generate is the single paid-work boundary; cache selection must
// precede it on a reuse round.
type recordingReuseRequest struct {
	Versions      [][]recordingReadyUnit
	Cached        []recordingcache.Entry
	Refresh       bool
	RNG           *rand.Rand
	BuildCache    func(context.Context, recordingcache.Entry) ([]recordingReadyUnit, bool)
	Generate      func(context.Context) ([]recordingReadyUnit, error)
	AllowFallback func(error) bool
}

type recordingReuseResult struct {
	Units         []recordingReadyUnit
	Versions      [][]recordingReadyUnit
	Generated     bool
	CacheHit      bool // A persistent entry was rebuilt; in-memory reuse is separate.
	CacheIndex    int  // Index in the request's Cached slice, or -1.
	GenerationErr error
}

var errRecordingReuseEmpty = errors.New("未取得可播放的话术声音单元")

// resolveRecordingReuse makes exactly one reuse/refresh decision. Successful
// generation is selected for this round, rather than added to a pool and then
// randomly discarded. A bad cached asset does not force paid regeneration when
// another valid candidate remains. The caller persists generated units and
// cache hit counts, and replaces its memory pool with result.Versions.
func resolveRecordingReuse(ctx context.Context, request recordingReuseRequest) (recordingReuseResult, error) {
	result := recordingReuseResult{CacheIndex: -1}
	for _, units := range request.Versions {
		if validRecordingReuseUnits(units) {
			result.Versions = append(result.Versions, units)
		}
	}
	// An immutable track identity may not be reused for different content.
	// Quarantine every conflicting memory candidate instead of making validity
	// depend on whichever version happened to be visited first.
	var clean [][]recordingReadyUnit
	for index, units := range result.Versions {
		conflict := false
		for other, candidate := range result.Versions {
			if other != index && conflictingRecordingReuseUnits(units, candidate) {
				conflict = true
				break
			}
		}
		if !conflict {
			clean = append(clean, units)
		}
	}
	result.Versions = clean
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if !request.Refresh {
		if selected, ok := selectRecordingReuse(ctx, request, result); ok {
			return selected, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var units []recordingReadyUnit
	err := errRecordingReuseEmpty
	if request.Generate != nil {
		units, err = request.Generate(ctx)
		if err == nil && (!validRecordingReuseUnits(units) || conflictsWithRecordingReusePool(units, result.Versions)) {
			err = errRecordingReuseEmpty
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err == nil {
		result.Units = units
		result.Generated = true
		result.Versions = rememberRecordingReuse(result.Versions, units)
		return result, nil
	}
	if liveContinuityTerminal(ctx, err) || (request.AllowFallback != nil && !request.AllowFallback(err)) {
		return result, err
	}
	result.GenerationErr = err
	if request.Refresh {
		if selected, ok := selectRecordingReuse(ctx, request, result); ok {
			return selected, nil
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}

func selectRecordingReuse(ctx context.Context, request recordingReuseRequest, result recordingReuseResult) (recordingReuseResult, bool) {
	count := len(result.Versions) + len(request.Cached)
	order := make([]int, count)
	if request.RNG != nil {
		order = request.RNG.Perm(count)
	} else {
		for index := range order {
			order[index] = index
		}
	}
	for _, index := range order {
		if ctx.Err() != nil {
			return result, false
		}
		if index < len(result.Versions) {
			result.Units = result.Versions[index]
			return result, true
		}
		if request.BuildCache == nil {
			continue
		}
		cacheIndex := index - len(result.Versions)
		conflict := false
		for other, entry := range request.Cached {
			if other != cacheIndex && conflictingRecordingCacheEntries(request.Cached[cacheIndex], entry) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		units, ok := request.BuildCache(ctx, request.Cached[cacheIndex])
		if ctx.Err() != nil {
			return result, false
		}
		if !ok || !validRecordingReuseUnits(units) || conflictsWithRecordingReusePool(units, result.Versions) {
			continue
		}
		result.Units = units
		result.CacheHit = true
		result.CacheIndex = cacheIndex
		result.Versions = rememberRecordingReuse(result.Versions, units)
		return result, true
	}
	return result, false
}

func rememberRecordingReuse(versions [][]recordingReadyUnit, units []recordingReadyUnit) [][]recordingReadyUnit {
	for index, existing := range versions {
		if sameRecordingReuseVersion(existing, units) {
			versions[index] = units // Keep freshly signed URLs with stable content.
			return versions
		}
	}
	versions = append(versions, units)
	if len(versions) > recordingCacheMaxVersions {
		versions = versions[len(versions)-recordingCacheMaxVersions:]
	}
	return versions
}

func validRecordingReuseUnits(units []recordingReadyUnit) bool {
	if len(units) == 0 {
		return false
	}
	seen := make(map[string]bool, len(units))
	for _, unit := range units {
		id := strings.TrimSpace(unit.Track.ID)
		if id == "" || seen[id] || strings.TrimSpace(unit.Text) == "" ||
			strings.TrimSpace(unit.Text) != strings.TrimSpace(unit.Track.Text) ||
			strings.TrimSpace(unit.Track.AudioURL) == "" || unit.Track.DurationMS <= 0 || len(unit.Track.Timeline) == 0 {
			return false
		}
		seen[id] = true
	}
	return true
}

func sameRecordingReuseUnit(left, right recordingReadyUnit) bool {
	// A URL signature is intentionally not immutable identity. It is renewed
	// when a persisted asset is rebuilt immediately before playback.
	left.Track.AudioURL, right.Track.AudioURL = "", ""
	return reflect.DeepEqual(left, right)
}

func sameRecordingReuseVersion(left, right []recordingReadyUnit) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if !sameRecordingReuseUnit(left[index], right[index]) {
			return false
		}
	}
	return true
}

func conflictingRecordingReuseUnits(left, right []recordingReadyUnit) bool {
	for _, a := range left {
		for _, b := range right {
			if a.Track.ID == b.Track.ID && !sameRecordingReuseUnit(a, b) {
				return true
			}
		}
	}
	return false
}

func conflictsWithRecordingReusePool(units []recordingReadyUnit, pool [][]recordingReadyUnit) bool {
	for _, existing := range pool {
		if conflictingRecordingReuseUnits(units, existing) {
			return true
		}
	}
	return false
}

func conflictingRecordingCacheEntries(left, right recordingcache.Entry) bool {
	parts := func(entry recordingcache.Entry) []recordingcache.Part {
		if len(entry.Parts) > 0 {
			return entry.Parts
		}
		return []recordingcache.Part{{Spoken: entry.Spoken, AssetID: entry.AssetID, Duration: entry.Duration, Segments: entry.Segments}}
	}
	for _, a := range parts(left) {
		for _, b := range parts(right) {
			if a.AssetID > 0 && a.AssetID == b.AssetID && !reflect.DeepEqual(a, b) {
				return true
			}
		}
	}
	return false
}
