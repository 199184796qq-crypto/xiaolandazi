package decisionexecutor

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestInsertSpeechPausesKeepsProductTogether(t *testing.T) {
	ttsPauseOnce = sync.Once{}
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "tts-pause-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TTS_PAUSE_RULES", path)
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", "")

	in := "咱卖的是南充特产绸都杨鸭子，卤味熟食，开袋即食"
	want := "咱卖的是，南充特产，绸都杨鸭子，卤味熟食，开袋即食"
	if got := sanitizeSpokenText(in); got != want {
		t.Fatalf("paused text = %q, want %q", got, want)
	}
}

func TestInsertSpeechPausesSkipsParticles(t *testing.T) {
	ttsPauseOnce = sync.Once{}
	t.Setenv("TTS_PAUSE_RULES", "")
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", "")
	if got := insertSpeechPauses("这是的测试"); got != "这是的测试" {
		t.Fatalf("particle should not be split: %q", got)
	}
}
