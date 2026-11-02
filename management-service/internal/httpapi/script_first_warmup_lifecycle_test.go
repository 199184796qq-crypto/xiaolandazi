package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/ttsgateway"
)

func warmupFixture(t *testing.T, native bool, room int64) (model.LiveAgentPlanVersion, volcengineScriptUnit, string, []byte) {
	t.Helper()
	modelName := "qwen-audio-3.1-tts-flash"
	if native {
		modelName = ttsgateway.VolcengineICL2
	}
	v := model.LiveAgentPlanVersion{PlanID: 4, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: modelName, VoiceID: "S_fixture", Rate: 1}, GenerationContext: map[string]any{"content_mode": "user_audio", "recording_voice_mode": recordingStreamMode}}
	text := strings.Repeat("首段正文。", 45)
	key := scriptFirstParagraphKey(v, text)
	scope, ok := recordingCacheScopeFor(14, room, v.PlanID)
	if !ok {
		t.Fatal("room scope invalid")
	}
	u := volcengineScriptUnit{Text: text, Key: key, Dir: filepath.Join(scope.CacheDir(scriptFirstNamespace(modelName), "paragraphs"), key)}
	dir, _ := planCacheDir(14, v.PlanID, modelName)
	source := filepath.Join(dir, key)
	if err := os.MkdirAll(source, 0750); err != nil {
		t.Fatal(err)
	}
	audio := filepath.Join(source, "paragraph.wav")
	if native {
		audio = filepath.Join(source, "job.json.wav")
	}
	writeTestWAV(t, audio, make([]int16, 24000), 24000)
	raw, err := os.ReadFile(audio)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if native {
		// Supplier checkpoint contract: the fingerprint is path-independent, so
		// relocating a paid job must preserve its identity and native timestamps.
		input, _ := json.Marshal([]any{ttsgateway.VolcengineICL2, v.VoiceIdentity.VoiceID, text, v.VoiceIdentity.Rate, "pcm-24000-v1", ttsgateway.VolcengineSynthesisProfile})
		fingerprint := sha256.Sum256(input)
		state := map[string]any{"version": 1, "complete": true, "voice_id": v.VoiceIdentity.VoiceID, "input_sha": hex.EncodeToString(fingerprint[:]), "audio_sha": hex.EncodeToString(sum[:]), "task_id": "fixture-paid", "subtitles": []ttsgateway.SpeechSubtitle{{Text: text, End: 1}}}
		if err = writeVolcengineJSON(filepath.Join(source, "job.json"), state); err != nil {
			t.Fatal(err)
		}
	} else if err = writeVolcengineJSON(filepath.Join(source, "paragraph-job.json"), scriptFirstPaidState{Key: key, Complete: true, SourceSHA: hex.EncodeToString(sum[:]), VoiceID: v.VoiceIdentity.VoiceID}); err != nil {
		t.Fatal(err)
	}
	return v, u, source, raw
}

func TestPlanWarmupHydratesBothSupplierContractsWithoutNewPurchase(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "qwen", true: "volcengine"}[native], func(t *testing.T) {
			t.Setenv("MEDIA_ROOT", t.TempDir())
			v, u, source, want := warmupFixture(t, native, 29)
			s := &Server{}
			if err := s.hydrateFirstParagraphFromPlan(t.Context(), 14, 29, v, u); err != nil {
				t.Fatal(err)
			}
			var file string
			if native {
				out, ok := ttsgateway.LoadLongTextResult(ttsgateway.SynthesizeRequest{Model: v.VoiceIdentity.Model, VoiceID: v.VoiceIdentity.VoiceID, Text: u.Text, Rate: 1, LongText: &ttsgateway.LongTextJob{StatePath: filepath.Join(u.Dir, "job.json"), SourceVoiceID: v.VoiceIdentity.VoiceID}})
				if !ok || len(out.Subtitles) != 1 || out.Subtitles[0].End != 1 {
					t.Fatal("native paid checkpoint/subtitles lost on relocation", out)
				}
				file = out.AudioFile
				if _, err := os.Stat(filepath.Join(source, "paragraph-job.json")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("native contract must not require Qwen checkpoint")
				}
			} else {
				called := false
				out, err := prepareScriptFirstPaid(t.Context(), u, func(context.Context, string) (ttsgateway.SynthesizeResponse, error) {
					called = true
					return ttsgateway.SynthesizeResponse{}, errors.New("must not synthesize")
				})
				if err != nil || called {
					t.Fatal("hydrated paid source was repurchased", err)
				}
				file = out.AudioFile
			}
			got, err := os.ReadFile(file)
			if err != nil || string(got) != string(want) {
				t.Fatal("paid PCM changed during copy", err)
			}
		})
	}
}

func TestPlanWarmupPreservesExistingRoomAndIsolatesChangedVoice(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	v, u, _, _ := warmupFixture(t, true, 29)
	if err := os.MkdirAll(u.Dir, 0750); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(u.Dir, "job.json")
	if err := os.WriteFile(marker, []byte("pending-room-paid-task"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	if err := s.hydrateFirstParagraphFromPlan(t.Context(), 14, 29, v, u); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(marker)
	if string(got) != "pending-room-paid-task" {
		t.Fatal("existing room checkpoint overwritten")
	}
	v.VoiceIdentity.VoiceID = "S_other"
	u.Key = scriptFirstParagraphKey(v, u.Text)
	u.Dir = filepath.Join(t.TempDir(), u.Key)
	if err := s.hydrateFirstParagraphFromPlan(t.Context(), 14, 30, v, u); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(u.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("different voice used previous paid source")
	}
}

func TestPlanWarmupDoesNotRepurchaseUncertainOrCorruptSource(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(map[bool]string{false: "qwen", true: "volcengine"}[native], func(t *testing.T) {
			t.Setenv("MEDIA_ROOT", t.TempDir())
			v, u, dir, _ := warmupFixture(t, native, 29)
			name := "paragraph-job.json"
			if native {
				name = "job.json"
			}
			for _, body := range []string{"{invalid", `{"complete":false}`, `{}`} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				if err := (&Server{}).hydrateFirstParagraphFromPlan(t.Context(), 14, 29, v, u); err == nil {
					t.Fatal("uncertain paid intent silently fell through to fresh purchase", body)
				}
				if _, err := os.Stat(u.Dir); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("incomplete source copied into room")
				}
			}
		})
	}
}

func TestPlanWarmupConcurrentHydrationAndCancellation(t *testing.T) {
	t.Setenv("MEDIA_ROOT", t.TempDir())
	v, u, source, want := warmupFixture(t, false, 29)
	unlock, err := lockVolcengineParagraph(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	err = (&Server{}).hydrateFirstParagraphFromPlan(ctx, 14, 29, v, u)
	cancel()
	unlock()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("waiting for existing warmup ignored cancellation", err)
	}
	var wg sync.WaitGroup
	errorsCh := make(chan error, 12)
	for i := 0; i < cap(errorsCh); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsCh <- (&Server{}).hydrateFirstParagraphFromPlan(t.Context(), 14, 29, v, u)
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(filepath.Join(u.Dir, "paragraph.wav"))
	if err != nil || string(got) != string(want) {
		t.Fatal("concurrent hydration damaged paid audio", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(u.Dir), ".warmup-*"))
	if err != nil || len(leftovers) != 0 {
		t.Fatal("temporary copies leaked", leftovers, err)
	}
}
