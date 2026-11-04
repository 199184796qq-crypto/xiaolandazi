package httpapi

import (
	"context"
	"errors"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/voiceasset"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVoiceCleanupLocalPathBoundary(t *testing.T) {
	root := t.TempDir()
	scope := voiceasset.Scope{Tenant: 14, Plan: 4, Category: "mainline"}
	key := voiceasset.Digest("paragraph")
	valid := filepath.Join(root, "14", "plans", "4", "recording-cache", "script-first-v1", "plan-paragraphs", "paragraphs", key)
	if e := os.MkdirAll(valid, 0700); e != nil {
		t.Fatal(e)
	}
	if e := validateVoiceSourcePath(root, valid, scope); e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{root, filepath.Join(root, "..", "escape"), filepath.Join(root, "15", "plans", "4", "recording-cache", "x", "paragraphs", key), filepath.Join(root, "14", "plans", "5", "recording-cache", "x", "paragraphs", key), filepath.Join(root, "14", "plans", "4", "recording-cache", "x", "paragraphs", "..")} {
		if validateVoiceSourcePath(root, path, scope) == nil {
			t.Fatal("unsafe deletion admitted", path)
		}
	}
	link := filepath.Join(root, "14", "plans", "4", "recording-cache", "link")
	if e := os.Symlink(t.TempDir(), link); e == nil {
		if validateVoiceSourcePath(root, filepath.Join(link, "paragraphs", key), scope) == nil {
			t.Fatal("symlink escaped")
		}
	}
}
func TestVoiceCleanupRetainsReceiptIdentityWithoutAudioBackup(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "paid")
	if e := os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"job.json": `{"task_id":"paid-task","complete":false,"api_key":"must-not-retain","audio_url":"https://supplier.invalid/?signature=private"}`, "paid.wav": "audio-payload-not-copied"} {
		if e := os.WriteFile(filepath.Join(source, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	j := voiceasset.Job{ID: voiceasset.Digest("operation"), Scope: voiceasset.Scope{Tenant: 14, Plan: 4, Category: "mainline"}}
	hash := voiceasset.Digest(source)
	if e := preserveVoiceLocalEvidence(root, source, j, hash); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(root, "voice-cleanup-evidence", j.ID, hash+".json"))
	if e != nil {
		t.Fatal(e)
	}
	text := string(data)
	if !strings.Contains(text, "paid-task") || strings.Contains(text, "must-not-retain") || strings.Contains(text, "signature=") || strings.Contains(text, "audio-payload-not-copied") {
		t.Fatal("receipt evidence leaked data or lost provider ID")
	}
}
func TestVoiceCleanupRuntimeKeyDoesNotMutateSavedPlan(t *testing.T) {
	original := model.LiveAgentPlanVersion{PlanID: 4, GenerationContext: map[string]any{"fact": "preserve"}}
	oldKey := scriptFirstParagraphKey(original, "正文")
	st := voiceasset.Stamp{Scope: voiceasset.Scope{Tenant: 14, Plan: 4, Category: "mainline"}, PlanEpoch: 1}
	runtime := original
	runtime.GenerationContext = map[string]any{"fact": "preserve", voiceCacheEpochContextKey: st}
	if scriptFirstParagraphKey(runtime, "正文") == oldKey || scriptFirstParagraphLegacyKey(runtime, "正文") != oldKey {
		t.Fatal("cache/cursor identities mixed")
	}
	if _, ok := original.GenerationContext[voiceCacheEpochContextKey]; ok {
		t.Fatal("saved plan was mutated")
	}
	if e := checkScriptGenerationGuard(voiceasset.WithStamp(context.Background(), st, func(context.Context, voiceasset.Stamp) error { return voiceasset.ErrCleared })); !errors.Is(e, voiceasset.ErrCleared) {
		t.Fatal("generation guard missed clear", e)
	}
}
