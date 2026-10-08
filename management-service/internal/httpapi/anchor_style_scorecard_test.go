package httpapi

import "testing"

func TestScorecardModelSelectionHonorsOperatorChoice(t *testing.T) {
	models := []anchorStyleWorkbenchModel{
		{ID: "qwen", Model: "qwen3.8-max", Enabled: true, HasAPIKey: true, IsDefault: true},
		{ID: "ds-flash", Model: "deepseek-v4-flash", Enabled: true, HasAPIKey: true},
		{ID: "ds-pro", Model: "deepseek-v4-pro", Enabled: true, HasAPIKey: true},
		{ID: "kimi", Model: "kimi-k3", Enabled: true, HasAPIKey: true},
		{ID: "disabled", Model: "deepseek-v4-pro", Enabled: false, HasAPIKey: true},
		{ID: "no-key", Model: "deepseek-v4-pro", Enabled: true},
	}
	if id, err := resolveAnchorStyleScorecardCompiler(models, ""); err != nil || id != "ds-pro" {
		t.Fatalf("first-use recommendation=%q err=%v", id, err)
	}
	for _, want := range []string{"qwen", "ds-flash", "ds-pro", "kimi"} {
		id, err := resolveAnchorStyleScorecardCompiler(models, want)
		if err != nil || id != want {
			t.Fatalf("operator selected %q but route=%q err=%v", want, id, err)
		}
	}
	for _, id := range []string{"missing", "disabled", "no-key"} {
		if _, err := resolveAnchorStyleScorecardCompiler(models, id); err == nil {
			t.Fatalf("invalid selection %q silently accepted", id)
		}
	}
	if id, err := resolveAnchorStyleScorecardCompiler(models[:1], ""); err != nil || id != "qwen" {
		t.Fatalf("no DeepSeek should still allow configured models: %q %v", id, err)
	}
}
