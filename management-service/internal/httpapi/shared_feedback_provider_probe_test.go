package httpapi

import (
	"context"
	"encoding/json"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"os"
	"testing"
	"time"
)

// Explicit historical feedback replay. No plan, queue, TTS or playback writes.
func TestSharedFeedbackFlashProviderProbe(t *testing.T) {
	if os.Getenv("SHARED_FEEDBACK_PROVIDER_PROBE") != "1" {
		t.Skip("explicit provider probe required")
	}
	input, output := os.Getenv("SHARED_FEEDBACK_INPUT"), os.Getenv("SHARED_FEEDBACK_OUTPUT")
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string               `json:"name"`
		Request agentgateway.Request `json:"request"`
	}
	if err = json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	store, err := appdb.Open(config.Load())
	if err != nil {
		t.Fatal("configuration database unavailable")
	}
	defer store.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cfg, err := store.AnchorGenerationSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Cycle.TextProfileID == "" || cfg.Cycle.FeedbackID != cfg.Cycle.TextProfileID {
		t.Fatal("mainline and feedback not shared")
	}
	profile, err := store.ResolveSpeechModel(ctx, cfg.Cycle.FeedbackID)
	if err != nil || profile == nil || profile.Model != "qwen3.7-flash" {
		t.Fatal("expected configured Flash connection")
	}
	rows := []map[string]any{}
	defer func() {
		data, _ := json.MarshalIndent(map[string]any{"passed": !t.Failed(), "shared_profile": profile.ID, "model": profile.Model, "text_only": true, "rows": rows}, "", "  ")
		if e := os.WriteFile(output, data, 0600); e != nil {
			t.Error(e)
		}
	}()
	mother, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ROLLING_FEEDBACK_PROMPT_PATH", "configs/prompts/anchor-style/rolling-feedback-generation.md", "回灌母版")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		c.Request.Model = profile.Model
		c.Request.Provider = "anchor:" + profile.ID
		c.Request.EnableThinking = false
		c.Request.Timeout = 45 * time.Second
		c.Request.Messages[0].Content = mother
		// Match completeCycleMacro: feedback uses its own task prompt, without the optional profile system prompt.
		response, callErr := agentgateway.CompleteSpeechEndpoint(ctx, *profile, c.Request)
		response.Provider = "anchor:" + profile.ID
		var packet struct {
			Report anchorstylescore.RollingReport `json:"report"`
		}
		if err = json.Unmarshal([]byte(c.Request.Messages[len(c.Request.Messages)-1].Content), &packet); err != nil {
			t.Fatal(err)
		}
		prompt, addresses, warnings, sources, decodeErr := normalizeRollingFeedbackDetailed(response.Text, packet.Report.AllowedAddresses, packet.Report.Issues)
		row := map[string]any{"name": c.Name, "request": c.Request, "response": response, "prompt": prompt, "addresses": addresses, "warnings": warnings, "correction_sources": sources, "structure_valid": decodeErr == nil, "success": callErr == nil}
		rows = append(rows, row)
		if callErr != nil {
			t.Fatal("feedback generation failed")
		}
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		for _, source := range sources {
			if source != "model" {
				t.Fatal("probe needed fallback instead of model guidance")
			}
		}
		t.Logf("case=%s model=%s input=%d output=%d latency_ms=%d structure_valid=true", c.Name, response.Model, response.InputTokens, response.OutputTokens, response.LatencyMS)
	}
}
