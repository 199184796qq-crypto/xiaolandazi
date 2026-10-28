package decisionexecutor

import (
	"path/filepath"
	"strings"
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

func TestInsertSpeechPausesDoesNotSplitBrandOnSecondPass(t *testing.T) {
	ttsPauseOnce = sync.Once{}
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "tts-pause-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TTS_PAUSE_RULES", path)
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", "")

	// This is the already-punctuated form that caused "绸都，杨鸭子" in TTS.
	in := "咱卖的是，南充特产，绸都杨鸭子，卤味熟食，开袋即食"
	want := "咱卖的是，南充特产，绸都杨鸭子，卤味熟食，开袋即食"
	if got := sanitizeSpokenText(in); got != want {
		t.Fatalf("brand was split again: %q, want %q", got, want)
	}
}

func TestInsertSpeechPausesRepairsModelSplitBrand(t *testing.T) {
	ttsPauseOnce = sync.Once{}
	path, err := filepath.Abs(filepath.Join("..", "..", "..", "configs", "prompts", "anchor-style", "tts-pause-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TTS_PAUSE_RULES", path)
	t.Setenv("ANCHOR_STYLE_PROMPT_DIR", "")

	in := "咱卖的是南充特产，绸都，杨鸭子，卤味熟食"
	got := sanitizeSpokenText(in)
	if !strings.Contains(got, "绸都杨鸭子") || strings.Contains(got, "绸都，杨鸭子") {
		t.Fatalf("split brand not repaired: %q", got)
	}
}

func TestAddressSSMLInsertsBreak(t *testing.T) {
	got, ok := addressSSML("小斌老乡来啦", "小斌老乡", "200ms")
	if !ok {
		t.Fatal("expected ssml")
	}
	want := `<speak>小斌老乡<break time="200ms"/>来啦</speak>`
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, ok := addressSSML("别的话", "小斌", "200ms"); ok {
		t.Fatal("missing address must not produce ssml")
	}
}
