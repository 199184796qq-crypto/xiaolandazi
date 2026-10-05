package decisionexecutor

import (
	"strings"
	"testing"
)

func TestSplitMaterialTextUsesBoundedChunks(t *testing.T) {
	text := strings.Repeat("这是直播素材的一句话。", 200)
	chunks := splitMaterialText(text, 120, 20)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got=%d", len(chunks))
	}
	for _, chunk := range chunks {
		if len([]rune(chunk)) > 120 {
			t.Fatalf("chunk too large: %d", len([]rune(chunk)))
		}
	}
}

func TestSplitMaterialTextPreservesShortText(t *testing.T) {
	text := "菜籽油适合日常炒菜。"
	chunks := splitMaterialText(text, 120, 20)
	if len(chunks) != 1 || chunks[0] != text {
		t.Fatalf("chunks=%v", chunks)
	}
}
