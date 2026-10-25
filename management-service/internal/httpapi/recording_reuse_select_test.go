package httpapi

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"reflect"
	"testing"

	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/recordingcache"
)

func recordingReuseFixtureUnit(id, text string) recordingReadyUnit {
	return recordingReadyUnit{Text: text, Track: coreclient.ProgramRefreshTrack{
		ID: id, Text: text, AudioURL: "fixture://" + id, DurationMS: 1000,
		Timeline: []coreclient.ProgramRefreshTimelineSegment{{SegmentID: "segment-1", Index: 1, Text: text, StartMS: 0, EndMS: 1000, SafeCut: true}},
	}}
}

func TestRecordingReuseCachedCandidatePrecedesPaidGenerateAndHydratesMemory(t *testing.T) {
	unit := recordingReuseFixtureUnit("recording-cache-42", "可以复用的原句。")
	generated := 0
	result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
		Cached: []recordingcache.Entry{{AssetID: 42}},
		BuildCache: func(context.Context, recordingcache.Entry) ([]recordingReadyUnit, bool) {
			return []recordingReadyUnit{unit}, true
		},
		Generate: func(context.Context) ([]recordingReadyUnit, error) {
			generated++
			return nil, errors.New("不应生成")
		},
	})
	if err != nil || generated != 0 || !result.CacheHit || result.Generated || result.CacheIndex != 0 ||
		len(result.Versions) != 1 || !reflect.DeepEqual(result.Units, []recordingReadyUnit{unit}) {
		t.Fatal("cache selection did not precede paid work", result, generated, err)
	}
}

func TestRecordingReuseTriesAnotherAssetBeforeChargingForBadCache(t *testing.T) {
	var visited []int64
	generated := 0
	result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
		Cached: []recordingcache.Entry{{AssetID: 41}, {AssetID: 42}},
		BuildCache: func(_ context.Context, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
			visited = append(visited, entry.AssetID)
			if entry.AssetID == 41 {
				return nil, false
			}
			return []recordingReadyUnit{recordingReuseFixtureUnit("asset-42", "第二份有效缓存。")}, true
		},
		Generate: func(context.Context) ([]recordingReadyUnit, error) { generated++; return nil, io.EOF },
	})
	if err != nil || generated != 0 || !result.CacheHit || result.CacheIndex != 1 || !reflect.DeepEqual(visited, []int64{41, 42}) {
		t.Fatal(result, visited, generated, err)
	}
}

func TestRecordingReuseRefreshAlwaysPlaysTheNewVersionThisRound(t *testing.T) {
	old := []recordingReadyUnit{recordingReuseFixtureUnit("old-41", "旧说法。")}
	fresh := []recordingReadyUnit{recordingReuseFixtureUnit("new-42", "这轮的新说法。")}
	for seed := int64(0); seed < 20; seed++ {
		builds, generated := 0, 0
		result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
			Refresh: true, Versions: [][]recordingReadyUnit{old}, Cached: []recordingcache.Entry{{AssetID: 43}}, RNG: rand.New(rand.NewSource(seed)),
			BuildCache: func(context.Context, recordingcache.Entry) ([]recordingReadyUnit, bool) { builds++; return old, true },
			Generate:   func(context.Context) ([]recordingReadyUnit, error) { generated++; return fresh, nil },
		})
		if err != nil || !result.Generated || result.CacheHit || builds != 0 || generated != 1 || !reflect.DeepEqual(result.Units, fresh) {
			t.Fatal("successful refresh was not selected", seed, result, builds, generated, err)
		}
	}
}

func TestRecordingReuseRefreshFailureMayUseValidPriorAndRetainsCause(t *testing.T) {
	old := []recordingReadyUnit{recordingReuseFixtureUnit("old-41", "仍然有效的旧说法。")}
	wantErr := errors.New("本轮可选更新未获预算")
	result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
		Refresh: true, Versions: [][]recordingReadyUnit{old},
		Generate: func(context.Context) ([]recordingReadyUnit, error) { return nil, wantErr },
	})
	if err != nil || result.Generated || result.CacheHit || !errors.Is(result.GenerationErr, wantErr) || !reflect.DeepEqual(result.Units, old) {
		t.Fatal(result, err)
	}
}

func TestRecordingReuseDoesNotHideTerminalOrUnknownOutcome(t *testing.T) {
	old := []recordingReadyUnit{recordingReuseFixtureUnit("old-41", "旧说法。")}
	for _, wantErr := range []error{anchorcycle.ErrStale, io.ErrUnexpectedEOF} {
		result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
			Refresh: true, Versions: [][]recordingReadyUnit{old},
			Generate:      func(context.Context) ([]recordingReadyUnit, error) { return nil, wantErr },
			AllowFallback: func(err error) bool { return !errors.Is(err, io.ErrUnexpectedEOF) },
		})
		if !errors.Is(err, wantErr) || len(result.Units) != 0 || result.Generated || result.CacheHit {
			t.Fatal("terminal/ambiguous outcome was hidden by old audio", result, err)
		}
	}
}

func TestRecordingReuseCancellationDuringBuildNeverReturnsPlayableFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	generated := 0
	result, err := resolveRecordingReuse(ctx, recordingReuseRequest{
		Cached: []recordingcache.Entry{{AssetID: 41}},
		BuildCache: func(context.Context, recordingcache.Entry) ([]recordingReadyUnit, bool) {
			cancel()
			return []recordingReadyUnit{recordingReuseFixtureUnit("old-41", "旧说法。")}, true
		},
		Generate: func(context.Context) ([]recordingReadyUnit, error) { generated++; return nil, nil },
	})
	if !errors.Is(err, context.Canceled) || len(result.Units) != 0 || generated != 0 {
		t.Fatal(result, generated, err)
	}
}

func TestRecordingReuseRejectsMissingIdentityAndConflictingImmutableContent(t *testing.T) {
	valid := recordingReuseFixtureUnit("new-43", "有效新单元。")
	missing := recordingReuseFixtureUnit("", "缺少标识。")
	wrongText := recordingReuseFixtureUnit("wrong-40", "原正文。")
	wrongText.Track.Text = "错误字幕。"
	conflictA := recordingReuseFixtureUnit("shared-41", "第一份正文。")
	conflictB := recordingReuseFixtureUnit("shared-41", "同标识另一份正文。")
	generated := 0
	result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
		Versions: [][]recordingReadyUnit{{missing}, {wrongText}, {conflictA}, {conflictB}},
		Generate: func(context.Context) ([]recordingReadyUnit, error) {
			generated++
			return []recordingReadyUnit{valid}, nil
		},
	})
	if err != nil || !result.Generated || generated != 1 || len(result.Versions) != 1 || result.Units[0].Track.ID != valid.Track.ID {
		t.Fatal(result, generated, err)
	}
}

func TestRecordingReuseEmptyOrInvalidGenerationIsNotSuccess(t *testing.T) {
	for _, units := range [][]recordingReadyUnit{nil, {recordingReuseFixtureUnit("", "无标识。")}} {
		result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
			Generate: func(context.Context) ([]recordingReadyUnit, error) { return units, nil },
		})
		if !errors.Is(err, errRecordingReuseEmpty) || result.Generated || len(result.Units) != 0 {
			t.Fatal(result, err)
		}
	}
}

func TestRecordingReuseMemoryPoolRenewsURLsWithoutDuplicatingImmutableVersion(t *testing.T) {
	old := recordingReuseFixtureUnit("old-41", "正文。")
	renewed := old
	renewed.Track.AudioURL = "fixture://new-signature"
	versions := rememberRecordingReuse([][]recordingReadyUnit{{old}}, []recordingReadyUnit{renewed})
	if len(versions) != 1 || versions[0][0].Track.AudioURL != renewed.Track.AudioURL || versions[0][0].Track.ID != old.Track.ID {
		t.Fatal(versions)
	}
	for _, id := range []string{"42", "43", "44", "45"} {
		versions = rememberRecordingReuse(versions, []recordingReadyUnit{recordingReuseFixtureUnit(id, id+"。")})
	}
	if len(versions) != recordingCacheMaxVersions || versions[0][0].Track.ID != "42" || versions[3][0].Track.ID != "45" {
		t.Fatal(versions)
	}
}

func TestRecordingReuseQuarantinesConflictingPersistedAssetTextBeforeBuild(t *testing.T) {
	var built []int64
	result, err := resolveRecordingReuse(context.Background(), recordingReuseRequest{
		Cached: []recordingcache.Entry{{AssetID: 41, Spoken: "原声音正文。"}, {AssetID: 41, Spoken: "被改过的另一份正文。"}, {AssetID: 42, Spoken: "有效缓存。"}},
		BuildCache: func(_ context.Context, entry recordingcache.Entry) ([]recordingReadyUnit, bool) {
			built = append(built, entry.AssetID)
			return []recordingReadyUnit{recordingReuseFixtureUnit("asset", entry.Spoken)}, true
		},
		Generate: func(context.Context) ([]recordingReadyUnit, error) {
			t.Fatal("valid cache remained; must not generate")
			return nil, nil
		},
	})
	if err != nil || !result.CacheHit || result.CacheIndex != 2 || !reflect.DeepEqual(built, []int64{42}) {
		t.Fatal("an immutable persisted asset was paired with conflicting text", result, built, err)
	}
}
