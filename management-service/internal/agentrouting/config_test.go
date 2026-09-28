package agentrouting

import "testing"

func TestDefaultConfigMatchesAdoptAndLearning(t *testing.T) {
	cfg := Default()
	if got := Match(cfg, "保存发布"); got != "adopt" {
		t.Fatalf("保存发布 intent=%q want adopt", got)
	}
	if got := Match(cfg, "这个回答太生硬，再自然一点"); got != "learning" {
		t.Fatalf("learning intent=%q want learning", got)
	}
	if got := Match(cfg, "今天有点累"); got != "" {
		t.Fatalf("ordinary chat must not deterministic-match, got %q", got)
	}
}

func TestDefaultJSONRoundTrip(t *testing.T) {
	cfg, err := Parse(DefaultJSON())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FallbackIntent != "chat" || !NaturalActionAllowed(cfg, "adopt") {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}
