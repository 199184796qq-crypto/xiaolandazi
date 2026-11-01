package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"livecompanion/management/internal/model"
	"livecompanion/management/internal/storage"
)

func TestScriptFirstUntimedArchiveWritesOnlyRealAudioAndOriginalText(t *testing.T) {
	root := t.TempDir()
	registry, err := storage.NewRegistry(storage.Config{Driver: "local", LocalRoot: root})
	if err != nil {
		t.Fatal(err)
	}
	v := model.LiveAgentPlanVersion{PlanID: 2, VoiceIdentity: model.LiveAgentVoiceIdentity{VoiceID: "voice", Model: "qwen-audio-3.1-tts-flash", Rate: 1}}
	pcm := make([]int16, 24000)
	pcm[0] = 123
	raw := scriptFirstPCMToWAV(pcm)
	var input model.CreateMediaAssetInput
	create := func(_ context.Context, in model.CreateMediaAssetInput) (model.MediaAsset, error) {
		input = in
		return model.MediaAsset{ID: 9}, nil
	}
	id, err := archiveUntimedScriptFirstAsset(t.Context(), registry, create, 1, 3, v, "paragraph-key", "完整原文。", "voice", raw, 1000)
	if err != nil || id != 9 {
		t.Fatal(id, err)
	}
	if input.Metadata["timeline_precision"] != "none" || input.Metadata["untimed_transcript"] != true || input.Metadata["original_transcript"] != "完整原文。" {
		t.Fatal(input.Metadata)
	}
	for _, key := range []string{"subtitle_object_key", "timeline_object_key", "safe_points_object_key"} {
		if _, ok := input.Metadata[key]; ok {
			t.Fatal("fake subtitle metadata", key)
		}
	}
	r, err := registry.Current().Open(t.Context(), input.ObjectKey)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := io.ReadAll(r)
	r.Close()
	if err != nil || !bytes.Equal(saved, raw) {
		t.Fatal("audio changed", err)
	}
	files := 0
	_ = filepath.WalkDir(root, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files++
		}
		return nil
	})
	if files != 1 {
		t.Fatal("unexpected SRT/sidecar", files)
	}
	create = func(context.Context, model.CreateMediaAssetInput) (model.MediaAsset, error) {
		return model.MediaAsset{}, errors.New("DB unavailable")
	}
	if _, err := archiveUntimedScriptFirstAsset(t.Context(), registry, create, 1, 3, v, "another-key", "原文。", "voice", raw, 1000); err == nil {
		t.Fatal("failed DB accepted")
	}
}

func TestScriptFirstUntimedTranscriptOncePerParagraphKeepsAllAudioChildren(t *testing.T) {
	now := time.Now()
	items := []model.SessionSpeechAsset{}
	for i, track := range []string{"long-9-1-u0-b0-raw", "long-9-1-u0-b1-raw", "long-9-1-u0-b2-raw", "long-9-2-u0-b0-raw"} {
		at := now.Add(time.Duration(i) * time.Second)
		items = append(items, model.SessionSpeechAsset{ID: int64(i + 1), Kind: "mainline", Text: "无字幕的完整段落。", SourceRef: model.SessionMainlineRef("program", track), PlayedAt: &at, AudioURL: "https://asset.invalid/audio.wav"})
	}
	if got := sessionTranscript(items); strings.Count(got, "无字幕的完整段落。") != 2 {
		t.Fatal("per-child text repeated or new replay lost", got)
	}
	if got := sessionDownloadItems(items); len(got) != 4 {
		t.Fatal("audio children removed", len(got))
	}
}
