package decisionexecutor

import (
	"strings"
	"testing"
)

func TestSanitizeSpokenText(t *testing.T) {
	in := "哎哟，你俩可算来啦！  \n咱这儿卖的是南平（南充）特产【限时】——卤味熟食，开袋即食。"
	got := sanitizeSpokenText(in)
	if strings.Contains(got, "（") || strings.Contains(got, "【") || strings.Contains(got, "—") {
		t.Fatalf("brackets or dash kept: %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("double space kept: %q", got)
	}
	if !strings.Contains(got, "南平特产") || !strings.Contains(got, "卤味熟食") {
		t.Fatalf("content mangled: %q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("line break should be kept as a pause: %q", got)
	}
}
