package httpapi

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func TestScriptFirstParagraphLimitsAndModes(t *testing.T) {
	for _, n := range []int{0, 1, 199, 200, 400, 2000, 2001} {
		err := validateScriptFirstTexts([]string{strings.Repeat("字", n)})
		if (err == nil) != (n >= 200 && n <= 2000) {
			t.Fatalf("%d: %v", n, err)
		}
	}
	if err := validateScriptFirstTexts([]string{strings.Repeat("甲", 200) + "====" + strings.Repeat("乙", 2000), strings.Repeat("丙", 400)}); err != nil {
		t.Fatal(err)
	}
	for _, modelName := range []string{ttsgateway.VolcengineICL2, "qwen-audio-3.1-tts-flash", "qwen-audio-3.0-tts-plus"} {
		v := model.LiveAgentPlanVersion{VoiceIdentity: model.LiveAgentVoiceIdentity{Model: modelName}, GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": recordingStreamMode}}
		if !usesScriptFirstParagraphs(v) {
			t.Fatal(modelName)
		}
		v.GenerationContext["recording_voice_mode"] = "original"
		if usesScriptFirstParagraphs(v) {
			t.Fatal("original recording must not enter TTS")
		}
	}
}

func TestScriptFirstFallbackExcludesCurrentAndDoesNotChangeFirstPass(t *testing.T) {
	known := map[int]volcengineCachedAudio{0: {AssetID: 10}, 1: {AssetID: 11}, 2: {AssetID: 12}}
	rng := rand.New(rand.NewSource(19))
	cursor := volcengineScriptCursor{Round: 0, Position: 1, Order: []int{0, 1, 2}}
	for i := 0; i < 100; i++ {
		x, ok := scriptFirstFallback(known, 1, 0, rng)
		if !ok || x != 2 {
			t.Fatal(x, ok)
		}
		if cursor.Position != 1 || cursor.Round != 0 {
			t.Fatal("fallback advanced source")
		}
	}
	for _, current := range []int{0, 1} {
		if x, ok := scriptFirstFallback(map[int]volcengineCachedAudio{0: {AssetID: 10}, 1: {AssetID: 11}}, current, 1-current, rng); !ok || x == current {
			t.Fatal("repeated current with alternate", x)
		}
	}
	if x, ok := scriptFirstFallback(map[int]volcengineCachedAudio{0: {AssetID: 10}}, 0, 0, rng); !ok || x != 0 {
		t.Fatal("single cache must remain playable")
	}
	if _, ok := scriptFirstFallback(nil, 0, 0, rng); ok {
		t.Fatal("empty cache")
	}
	if got := scriptFirstPlayingUnit(scriptFirstTrackID(12, 5, 3, 2), 12); got != 3 {
		t.Fatal(got)
	}
	if got := scriptFirstPlayingUnit(scriptFirstTrackID(12, 5, 3, 2), 13); got != -1 {
		t.Fatal("cross-session ID")
	}
}

func TestScriptFirstPaidResultSurvivesStopAndArchiveRetry(t *testing.T) {
	u := volcengineScriptUnit{Key: "paragraph", Dir: t.TempDir()}
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	generate := func(_ context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
		calls++
		if err := os.WriteFile(path, []byte("paid-audio"), 0600); err != nil {
			t.Fatal(err)
		}
		cancel()
		return ttsgateway.SynthesizeResponse{AudioFile: path, VoiceID: "voice", Subtitles: []ttsgateway.SpeechSubtitle{{Text: "原文", End: 1}}}, nil
	}
	if _, err := prepareScriptFirstPaid(ctx, u, generate); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		out, err := prepareScriptFirstPaid(t.Context(), u, generate)
		if err != nil || out.VoiceID != "voice" || len(out.Subtitles) != 1 {
			t.Fatal(out, err)
		}
	}
	if calls != 1 {
		t.Fatal("purchased again", calls)
	}
	if err := os.WriteFile(filepath.Join(u.Dir, "paragraph.wav"), []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := prepareScriptFirstPaid(t.Context(), u, generate); err != nil {
		t.Fatal("unavailable paid media did not receive replacement", err)
	}
	if calls != 2 {
		t.Fatal("replacement was not synthesized exactly once", calls)
	}
}

func TestScriptFirstUnknownSubmitDoesNotPermanentlyFenceReplacement(t *testing.T) {
	u := volcengineScriptUnit{Key: "unknown", Dir: t.TempDir()}
	calls := 0
	generate := func(context.Context, string) (ttsgateway.SynthesizeResponse, error) {
		calls++
		return ttsgateway.SynthesizeResponse{}, errors.New("connection lost after send")
	}
	for i := 0; i < 3; i++ {
		if _, err := prepareScriptFirstPaid(t.Context(), u, generate); err == nil {
			t.Fatal("unknown success")
		}
	}
	if calls != 3 {
		t.Fatal("each retry should replace the unavailable completed attempt", calls)
	}
	backups, _ := filepath.Glob(filepath.Join(u.Dir, ".unavailable-*", "paragraph-job.json"))
	if len(backups) != 2 {
		t.Fatal("old attempts not retained", backups)
	}
}

func TestScriptFirstRetryBackoffIsBounded(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	for i := 0; i < 100; i++ {
		d := scriptFirstRetryDelay(i, rng)
		base := time.Second << min(i, 3)
		if d < base*8/10 || d > base*12/10 {
			t.Fatal(i, d)
		}
	}
}

func TestScriptFirstPlaybackQueueResumesSameChildAfterLostAckAndStop(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	blocks := []scriptFirstCachedBlock{{AssetID: 1}, {AssetID: 2}, {AssetID: 3}}
	pending := &volcenginePendingTrack{Unit: 0, Advance: true}
	accepted := map[int]int{}
	lost := true
	saves := 0
	deliver := func(_ context.Context, index int, _ scriptFirstCachedBlock) error {
		if accepted[index] == 0 {
			accepted[index] = 1
		}
		if index == 1 && lost {
			lost = false
			return errors.New("accepted but acknowledgement lost")
		}
		return nil
	}
	save := func() error {
		saves++
		if pending.NextBlock == 2 {
			cancel()
		}
		return nil
	}
	if err := publishScriptFirstBlocks(ctx, pending, blocks, deliver, save); err == nil || pending.NextBlock != 1 {
		t.Fatal(pending, err)
	}
	if err := publishScriptFirstBlocks(ctx, pending, blocks, deliver, save); !errors.Is(err, context.Canceled) || pending.NextBlock != 2 {
		t.Fatal(pending, err)
	}
	if len(accepted) != 2 {
		t.Fatal("late child published", accepted)
	}
	if err := publishScriptFirstBlocks(t.Context(), pending, blocks, deliver, save); err != nil || pending.NextBlock != 3 {
		t.Fatal(pending, err)
	}
	if len(accepted) != 3 || saves != 3 {
		t.Fatal(accepted, saves)
	}
}
