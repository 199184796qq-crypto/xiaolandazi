package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/config"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/stylecontract"
)

// This command is an operator-only, stdin/stdout probe. It exercises the same
// model resolver as the service without persisting plans, facts, scripts or
// generated speech.
type probeInput struct {
	Action         string                                `json:"action"`
	Provider       string                                `json:"provider"`
	Stage          string                                `json:"stage"`
	System         string                                `json:"system"`
	User           string                                `json:"user"`
	MaxTokens      int                                   `json:"max_tokens"`
	ResponseFormat string                                `json:"response_format"`
	SourceText     string                                `json:"source_text"`
	AnchorStyle    model.LiveAgentPlanAnchorStyleProfile `json:"anchor_style"`
	Candidates     []string                              `json:"candidates"`
	TargetChars    int                                   `json:"target_chars"`
	Heat           int                                   `json:"heat"`
	Scene          stylecontract.RuntimeScene            `json:"scene"`
}

type probeOutput struct {
	Text         string `json:"text"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	LatencyMS    int64  `json:"latency_ms"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

func main() {
	var input probeInput
	decoder := json.NewDecoder(os.Stdin)
	if err := decoder.Decode(&input); err != nil {
		fatal(fmt.Errorf("decode probe input: %w", err))
	}
	if input.Action == "evaluate" {
		evaluate(input)
		return
	}

	cfg := config.Load()
	store, err := appdb.Open(cfg)
	if err != nil {
		fatal(fmt.Errorf("open management database: %w", err))
	}
	defer store.Close()
	if input.Action == "config" {
		configuration, err := store.SpeechModels(context.Background())
		if err != nil {
			fatal(err)
		}
		encode(configuration)
		return
	}
	if input.Action == "policy" {
		ctx := context.Background()
		l1, err := store.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL1, "", 0, 0)
		if err != nil {
			fatal(err)
		}
		industries, err := store.ListLivePolicyIndustries(ctx)
		if err != nil {
			fatal(err)
		}
		l2 := map[string]*model.LivePolicyVersion{}
		for _, industry := range industries {
			version, loadErr := store.GetActiveLivePolicyVersion(ctx, model.LivePolicyLayerL2, industry.Code, 0, 0)
			if loadErr != nil {
				fatal(loadErr)
			}
			if version != nil {
				l2[industry.Code] = version
			}
		}
		encode(map[string]any{"l1": l1, "l2": l2, "industries": industries})
		return
	}
	if input.Action == "models" {
		profile, err := store.ResolveSpeechModel(context.Background(), "")
		if err != nil || profile == nil {
			if err == nil {
				err = fmt.Errorf("configured anchor model is empty")
			}
			fatal(err)
		}
		models, err := agentgateway.ListSpeechEndpointModels(context.Background(), *profile)
		if err != nil {
			fatal(err)
		}
		encode(map[string]any{"configured_model": profile.Model, "models": models})
		return
	}
	if strings.TrimSpace(input.User) == "" {
		fatal(fmt.Errorf("user prompt is required"))
	}

	provider := strings.TrimSpace(input.Provider)
	if provider == "configured_anchor" {
		// The anchor: prefix forces the database-backed default even for style
		// analysis. This is intentional for offline A/B tests of the configured
		// anchor model; the regular service routing remains unchanged.
		provider = "anchor:"
	}
	stage := strings.TrimSpace(input.Stage)
	if stage == "" {
		stage = "style_analysis"
	}
	maxTokens := input.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	format := agentgateway.ResponseText
	if input.ResponseFormat == "json" {
		format = agentgateway.ResponseJSON
	}
	messages := make([]agentgateway.Message, 0, 2)
	if strings.TrimSpace(input.System) != "" {
		messages = append(messages, agentgateway.Message{Role: "system", Content: input.System})
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: input.User})

	ctx, cancel := context.WithTimeout(context.Background(), 175*time.Second)
	defer cancel()
	if input.Action == "raw" {
		profileID := ""
		if strings.HasPrefix(provider, "anchor:") {
			profileID = strings.TrimPrefix(provider, "anchor:")
		}
		profile, err := store.ResolveSpeechModel(ctx, profileID)
		if err != nil || profile == nil {
			if err == nil {
				err = fmt.Errorf("configured anchor model is empty")
			}
			fatal(err)
		}
		request := agentgateway.Request{Stage: stage, Model: profile.Model, Messages: messages, MaxTokens: maxTokens, EnableThinking: false, ResponseFormat: format, Timeout: 170 * time.Second}
		// Use the exact production speech-model transport. Besides making this
		// probe representative, it preserves provider-specific low-latency options
		// such as Qwen 3.8 reasoning_effort=none on official DashScope endpoints.
		result, err := agentgateway.CompleteSpeechEndpoint(ctx, *profile, request)
		if err != nil {
			fatal(err)
		}
		encode(probeOutput{Text: strings.TrimSpace(result.Text), Provider: "anchor:" + profile.ID, Model: profile.Model, LatencyMS: result.LatencyMS, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens, TotalTokens: result.TotalTokens})
		return
	}
	result, err := agentgateway.NewFromEnv().WithSpeechModels(store).Complete(ctx, agentgateway.Request{
		Stage:          stage,
		Provider:       provider,
		Messages:       messages,
		MaxTokens:      maxTokens,
		EnableThinking: false,
		ResponseFormat: format,
		Timeout:        170 * time.Second,
	})
	if err != nil {
		fatal(err)
	}
	encode(probeOutput{
		Text:         strings.TrimSpace(result.Text),
		Provider:     result.Provider,
		Model:        result.Model,
		LatencyMS:    result.LatencyMS,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		TotalTokens:  result.TotalTokens,
	})
}

func evaluate(input probeInput) {
	rawPurity := stylecontract.AssessPurity(input.AnchorStyle)
	profile := stylecontract.Normalize(input.AnchorStyle, input.SourceText)
	budget := stylecontract.CompileRuntimeBudget(profile, stylecontract.RuntimeOptions{
		TargetChars: input.TargetChars,
		Heat:        input.Heat,
		Scene:       input.Scene,
	})
	evaluations := make([]stylecontract.RuntimeEvaluation, 0, len(input.Candidates))
	for _, candidate := range input.Candidates {
		evaluations = append(evaluations, stylecontract.EvaluateRuntimeCandidate(budget, input.SourceText, candidate))
	}
	encode(map[string]any{
		"anchor_style":    profile,
		"raw_purity":      rawPurity,
		"compiled_purity": stylecontract.AssessPurity(profile),
		"runtime_budget":  budget,
		"evaluations":     evaluations,
		"coverage_errors": stylecontract.CoverageErrors(profile, input.SourceText),
	})
}

func encode(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]string{"error": err.Error()})
	os.Exit(1)
}
