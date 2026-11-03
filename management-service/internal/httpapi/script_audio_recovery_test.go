package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/scriptplay"
	"livecompanion/management/internal/storage"
	"livecompanion/management/internal/ttsgateway"
)

func TestUnavailableParagraphConcurrentRecoveryPurchasesOnce(t *testing.T) {
	u := volcengineScriptUnit{Key: "same-identity", Dir: t.TempDir()}
	if err := writeVolcengineJSON(filepath.Join(u.Dir, "paragraph-job.json"), scriptFirstPaidState{Key: u.Key, Complete: true, SourceSHA: "old-missing"}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := lockVolcengineParagraph(t.Context(), u.Dir)
			if err != nil {
				t.Error(err)
				return
			}
			defer release()
			out, err := prepareScriptFirstPaid(t.Context(), u, func(_ context.Context, path string) (ttsgateway.SynthesizeResponse, error) {
				calls.Add(1)
				return ttsgateway.SynthesizeResponse{AudioFile: path, VoiceID: "current"}, os.WriteFile(path, []byte("new-paid-wave"), 0600)
			})
			if err != nil || out.VoiceID != "current" {
				t.Error(out, err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent rooms duplicated recovery", calls.Load())
	}
	backups, _ := filepath.Glob(filepath.Join(u.Dir, ".unavailable-*", "paragraph-job.json"))
	if len(backups) != 1 {
		t.Fatal(backups)
	}
}

func TestUnavailableParagraphCancelledDoesNotStartReplacement(t *testing.T) {
	u := volcengineScriptUnit{Key: "cancel", Dir: t.TempDir()}
	path := filepath.Join(u.Dir, "paragraph-job.json")
	want := []byte(`{"key":"cancel","complete":false}`)
	if err := os.WriteFile(path, want, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := prepareScriptFirstPaid(ctx, u, func(context.Context, string) (ttsgateway.SynthesizeResponse, error) {
		t.Fatal("cancelled room purchased audio")
		return ttsgateway.SynthesizeResponse{}, nil
	})
	got, _ := os.ReadFile(path)
	if err == nil || string(got) != string(want) {
		t.Fatal(err, string(got))
	}
}

func TestCorruptIndexRetainedAndCanBeRebuilt(t *testing.T) {
	u := volcengineScriptUnit{Key: "index", Dir: t.TempDir()}
	if err := os.WriteFile(filepath.Join(u.Dir, "cache.json"), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	cache, err := loadRecoverableScriptCache(t.Context(), u)
	if err != nil || cache.Key != u.Key || len(cache.Audio) != 0 {
		t.Fatal(cache, err)
	}
	files, _ := filepath.Glob(filepath.Join(u.Dir, ".unavailable-*", "cache.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
}

func TestArchivedParagraphRequiresExactIdentityAndHealthyObject(t *testing.T) {
	for _, kind := range []string{"ready", "missing", "corrupt", "tenant", "plan", "voice", "model", "text", "key", "deleted"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			reg, err := storage.NewRegistry(storage.Config{Driver: "local", LocalRoot: root})
			if err != nil {
				t.Fatal(err)
			}
			v := model.LiveAgentPlanVersion{PlanID: 4, VoiceIdentity: model.LiveAgentVoiceIdentity{VoiceID: "selected", Model: "qwen-audio-3.1-tts-flash"}}
			u := volcengineScriptUnit{Key: "full-identity", Text: "完整正文。"}
			raw := scriptFirstPCMToWAV(make([]int16, 24000))
			sum := sha256.Sum256(raw)
			duration := uint64(1000)
			a := model.MediaAsset{ID: 7, TenantID: 14, AssetType: "generated_voice", Status: "active", StorageDriver: "local", ObjectKey: "archive.wav", SizeBytes: uint64(len(raw)), DurationMS: &duration, ChecksumSHA256: hex.EncodeToString(sum[:]), Metadata: map[string]any{"plan_id": float64(4), "paragraph_key": u.Key, "voice_id": "selected", "tts_model": v.VoiceIdentity.Model, "original_transcript": u.Text, "untimed_transcript": true}}
			if err := reg.Current().Put(t.Context(), a.ObjectKey, strings.NewReader(string(raw)), "audio/wav"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing":
				_ = reg.Current().Delete(t.Context(), a.ObjectKey)
			case "corrupt":
				a.ChecksumSHA256 = strings.Repeat("0", 64)
			case "tenant":
				a.TenantID = 15
			case "plan":
				a.Metadata["plan_id"] = 5
			case "voice":
				a.Metadata["voice_id"] = "other"
			case "model":
				a.Metadata["tts_model"] = "other"
			case "text":
				a.Metadata["original_transcript"] = "其它正文"
			case "key":
				a.Metadata["paragraph_key"] = "other"
			case "deleted":
				a.Status = "deleted"
			}
			got, ok := recoverScriptArchiveObject(t.Context(), reg, 14, v, u, a)
			if ok != (kind == "ready") || (ok && (got.AssetID != 7 || !got.UntimedTranscript)) {
				t.Fatal(got, ok)
			}
		})
	}
}

func TestRepairedPoolAudioKeepsRewriteAndPlaybackHistory(t *testing.T) {
	root := t.TempDir()
	v := model.LiveAgentPlanVersion{PlanID: 4, VoiceIdentity: model.LiveAgentVoiceIdentity{Model: "qwen", VoiceID: "selected"}}
	u := volcengineScriptUnit{Pool: "stable-piece"}
	old := scriptplay.Version{Kind: scriptplay.KindRewrite, Text: "改写正文", Fingerprint: "text-identity", FactsKey: "facts", VoiceID: "selected", Model: "qwen", AudioRef: "1", DurationMS: 1000, PlayCount: 7, CompleteCount: 4, SourcePath: "paid-source"}
	if err := addScriptVoiceVersion(root, 4, u.Pool, old); err != nil {
		t.Fatal(err)
	}
	if err := replaceScriptPoolAudio(root, v, u, 1, volcengineCachedAudio{AssetID: 2, DurationMS: 1200}); err != nil {
		t.Fatal(err)
	}
	p, err := loadScriptVoicePool(root, 4, u.Pool)
	if err != nil || len(p.Versions) != 1 {
		t.Fatal(p, err)
	}
	x := p.Versions[0]
	if x.AudioRef != "2" || x.DurationMS != 1200 || x.Kind != scriptplay.KindRewrite || x.Text != old.Text || x.FactsKey != old.FactsKey || x.PlayCount != 7 || x.CompleteCount != 4 {
		t.Fatal("replacement changed text meaning/type/history", x)
	}
}
