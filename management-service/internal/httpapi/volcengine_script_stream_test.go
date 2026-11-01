package httpapi

import (
	"context"
	"errors"
	"math/rand"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"livecompanion/management/internal/coreclient"
	"livecompanion/management/internal/model"
)

func TestVolcengineParagraphsAcrossTabsAndFirstRound(t *testing.T) {
	v := model.LiveAgentPlanVersion{GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": "stream_clone", "recording_script": map[string]any{"tabs": []any{
		map[string]any{"tab_id": "t1", "text": "第一句。\n第二句。==== 第三句！===="},
		map[string]any{"tab_id": "t2", "text": "====第四句？====第五句。"},
	}}}}
	if err := normalizeRecordingScript(v.GenerationContext); err != nil {
		t.Fatal(err)
	}
	units := volcengineScriptUnits(v, t.TempDir())
	want := []string{"第一句。\n第二句。", "第三句！", "第四句？", "第五句。"}
	var got []string
	for _, u := range units {
		got = append(got, u.Text)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	rng := rand.New(rand.NewSource(17))
	previous := -1
	for round := 0; round < 100; round++ {
		order := volcengineParagraphOrder(4, round, previous, rng)
		seen := map[int]bool{}
		for _, i := range order {
			if seen[i] {
				t.Fatal("duplicate paragraph")
			}
			seen[i] = true
		}
		if round == 0 && !reflect.DeepEqual(order, []int{0, 1, 2, 3}) {
			t.Fatal("first pass shuffled")
		}
		if round > 0 && order[0] == previous {
			t.Fatal("cycle boundary repeats")
		}
		previous = order[3]
	}
	if got := volcengineParagraphOrder(1, 5, 0, rng); !reflect.DeepEqual(got, []int{0}) {
		t.Fatal(got)
	}
}
func TestVolcengineFortySecondPaidGate(t *testing.T) {
	for _, tc := range []struct {
		ms  int64
		due bool
	}{{104000, false}, {40001, false}, {40000, false}, {39999, true}, {15000, true}, {0, true}} {
		if got := volcengineSupplyDue(coreclient.ProgramRefreshSnapshot{BufferedMainlineMS: tc.ms}); got != tc.due {
			t.Fatalf("buffer %d due %v", tc.ms, got)
		}
	}
}
func TestVolcengineWaitingReplaysWithoutRepeatingSynthesis(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	var submitted, replays atomic.Int32
	done := make(chan struct{})
	audio, err := awaitVolcengineParagraph(ctx, time.Millisecond, func(c context.Context) (volcengineCachedAudio, error) {
		submitted.Add(1)
		select {
		case <-done:
			return volcengineCachedAudio{AssetID: 8}, nil
		case <-c.Done():
			return volcengineCachedAudio{}, c.Err()
		}
	}, func(context.Context) error {
		if replays.Add(1) == 3 {
			close(done)
		}
		return nil
	})
	if err != nil || audio.AssetID != 8 || submitted.Load() != 1 || replays.Load() < 3 {
		t.Fatal(audio, err, submitted.Load(), replays.Load())
	}
}
func TestVolcengineStoppedDoesNotPublishLateResult(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { <-started; cancel(); close(release) }()
	audio, err := awaitVolcengineParagraph(ctx, time.Millisecond, func(context.Context) (volcengineCachedAudio, error) {
		close(started)
		<-release
		defer close(finished)
		return volcengineCachedAudio{AssetID: 9}, nil
	}, func(context.Context) error { return nil })
	if !errors.Is(err, context.Canceled) || audio.AssetID != 0 {
		t.Fatal("late media escaped", audio, err)
	}
	<-finished
}
func TestVolcengineCacheIdentityAndCursorIsolation(t *testing.T) {
	a := volcengineParagraphKey("S_a", "原文", 1)
	for _, b := range []string{volcengineParagraphKey("S_b", "原文", 1), volcengineParagraphKey("S_a", "原文改动", 1), volcengineParagraphKey("S_a", "原文", 1.01)} {
		if a == b {
			t.Fatal("cross voice/text/rate cache")
		}
	}
	c := volcengineScriptCursor{VersionID: 7, Signature: a, Order: []int{0, 1}, Previous: -1}
	if !validVolcengineCursor(c, 7, a, 2) || validVolcengineCursor(c, 8, a, 2) || validVolcengineCursor(c, 7, "other", 2) {
		t.Fatal("bad cursor fence")
	}
	c.Order = []int{1, 0}
	if validVolcengineCursor(c, 7, a, 2) {
		t.Fatal("first pass shuffled checkpoint")
	}
	c.Round = 1
	if !validVolcengineCursor(c, 7, a, 2) {
		t.Fatal("random loop rejected")
	}
	c.Order = []int{1, 1}
	if validVolcengineCursor(c, 7, a, 2) {
		t.Fatal("duplicate order")
	}
	path := filepath.Join(t.TempDir(), "cache.json")
	cache := volcengineParagraphCache{Key: a, Audio: map[int]volcengineCachedAudio{100: {AssetID: 5, DurationMS: 2000}}}
	if err := writeVolcengineJSON(path, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVolcengineParagraphCache(path, "different"); err == nil {
		t.Fatal("mismatched cache accepted")
	}
	got, err := loadVolcengineParagraphCache(path, a)
	if err != nil || got.Audio[100].AssetID != 5 {
		t.Fatal(got, err)
	}
}
