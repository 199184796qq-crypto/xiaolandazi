package decisionexecutor

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestInteractionSpeechSkipsTTSInsertedPauses(t *testing.T) {
	ttsPauseOnce = sync.Once{}
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "tts-pause-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TTS_PAUSE_RULES", path)
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", "")
	text := "咱卖的是南充特产绸都杨鸭子"
	mainline := sanitizeSpokenText(text)
	interaction := sanitizeInteractionSpokenText(text)
	if mainline == interaction {
		t.Fatalf("mainline pause behavior changed unexpectedly: %q", mainline)
	}
	if strings.Contains(interaction, "，") {
		t.Fatalf("interaction text got pause punctuation: %q", interaction)
	}
	if interaction != text {
		t.Fatalf("interaction text should only be cleaned, got %q", interaction)
	}
}

func TestIsInteractionSpeech(t *testing.T) {
	for _, item := range []decisionItem{
		{MissionKind: "reply_question"},
		{MissionKind: "welcome_named"},
		{Scene: "product_question"},
		{Scene: "complaint"},
	} {
		if !isInteractionSpeech(item) {
			t.Fatalf("expected interaction: %+v", item)
		}
	}
	for _, item := range []decisionItem{
		{MissionKind: "mainline"},
		{Scene: "opening"},
		{},
	} {
		if isInteractionSpeech(item) {
			t.Fatalf("expected mainline: %+v", item)
		}
	}
}
