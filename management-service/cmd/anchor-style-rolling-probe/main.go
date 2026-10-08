// Operator smoke test: read confirmed assets, call one configured feedback API,
// and return the raw result. Never writes assets, modifies Agents or sends speech.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
)

func main() {
	var input struct {
		Tenant  int64    `json:"tenant_id"`
		Plan    int64    `json:"plan_id"`
		Profile string   `json:"profile_id"`
		Texts   []string `json:"texts"`
		Start   int      `json:"start_index"`
	}
	check(json.NewDecoder(os.Stdin).Decode(&input))
	if input.Tenant < 1 || input.Plan < 1 || len(input.Texts) < 1 || len(input.Texts) > 5 || input.Start < 1 {
		check(fmt.Errorf("invalid scoped probe input"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	store, err := appdb.Open(config.Load())
	check(err)
	defer store.Close()
	meta, err := store.GetActiveLiveAnchorStyleMetaPrompt(ctx, input.Tenant, input.Plan)
	check(err)
	card, err := store.GetActiveLiveAnchorStyleScorecard(ctx, input.Tenant, input.Plan)
	check(err)
	if meta.Status != "confirmed" || card.Status != "confirmed" || card.MetaPromptAssetID != meta.ID {
		check(fmt.Errorf("confirmed matching assets required"))
	}
	b, err := anchorstylescore.Parse(card.EffectiveScorecardJSON())
	check(err)
	if input.Profile == "" {
		models, err := store.SpeechModels(ctx)
		check(err)
		for _, model := range models.Profiles {
			if model.Enabled && model.HasAPIKey && regexp.MustCompile(`(?i)flash|turbo|lite`).MatchString(model.Model) {
				input.Profile = model.ID
				break
			}
		}
	}
	if input.Profile == "" {
		check(fmt.Errorf("select a configured feedback profile"))
	}
	profile, err := store.ResolveSpeechModel(ctx, input.Profile)
	check(err)
	if profile == nil || !profile.Enabled {
		check(fmt.Errorf("selected profile unavailable"))
	}
	report := anchorstylescore.AnalyzeRolling(input.Texts, input.Start, b, meta.EffectiveMetaPromptMD())
	md, err := os.ReadFile(filepath.Join("configs", "prompts", "anchor-style", "rolling-feedback-generation.md"))
	check(err)
	categories := []string{}
	for _, category := range []string{"opening", "style", "ending"} {
		for _, issue := range report.Issues {
			if issue.Category == category {
				categories = append(categories, category)
				break
			}
		}
	}
	data, err := json.Marshal(map[string]any{"style_meta_prompt": meta.EffectiveMetaPromptMD(), "report": report, "expected_categories": categories, "recent_texts": input.Texts})
	check(err)
	result := agentgateway.Response{}
	if len(report.Issues) > 0 {
		result, err = agentgateway.CompleteSpeechEndpoint(ctx, *profile, agentgateway.Request{Stage: "rolling_style_feedback", Model: profile.Model, Messages: []agentgateway.Message{{Role: "system", Content: string(md)}, {Role: "user", Content: string(data)}}, ResponseFormat: agentgateway.ResponseJSON, MaxTokens: min(1000, profile.MaxTokens), EnableThinking: false, Timeout: time.Duration(profile.TimeoutMS) * time.Millisecond})
		check(err)
	}
	check(json.NewEncoder(os.Stdout).Encode(map[string]any{"profile_id": profile.ID, "model": profile.Model, "style_asset_id": meta.ID, "scorecard_asset_id": card.ID, "report": report, "raw_response": result.Text, "latency_ms": result.LatencyMS, "input_tokens": result.InputTokens, "output_tokens": result.OutputTokens}))
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
