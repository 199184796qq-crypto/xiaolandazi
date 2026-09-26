package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"livecompanion/management/internal/agentgateway"
)

type devContinuityQuality struct {
	Score       int      `json:"score"`
	Level       string   `json:"level"`
	Fatal       bool     `json:"fatal"`
	FatalReason string   `json:"fatal_reason,omitempty"`
	Summary     string   `json:"summary"`
	Issues      []string `json:"issues"`
}

func normalizeDevContinuityQuality(value devContinuityQuality) devContinuityQuality {
	if value.Score < 0 {
		value.Score = 0
	}
	if value.Score > 100 {
		value.Score = 100
	}
	value.FatalReason = strings.TrimSpace(value.FatalReason)
	if value.Fatal && value.Score > 59 {
		// Semantic misunderstanding or unsupported factual claims are not allowed
		// to pass merely because entry/resume continuity sounds natural.
		value.Score = 59
	}
	switch {
	case value.Score >= 90:
		value.Level = "HIGH"
	case value.Score >= 80:
		value.Level = "MEDIUM_HIGH"
	case value.Score >= 65:
		value.Level = "MEDIUM"
	default:
		value.Level = "LOW"
	}
	value.Summary = strings.TrimSpace(value.Summary)
	value.Issues = normalizeUniqueStrings(value.Issues, 8)
	return value
}

func decodeDevContinuityQuality(raw string) (devContinuityQuality, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```JSON")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var value devContinuityQuality
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		return devContinuityQuality{}, err
	}
	return normalizeDevContinuityQuality(value), nil
}

func devContinuityQualityPass(value devContinuityQuality) bool {
	return !value.Fatal && value.Score >= 80
}

func effectiveReviewProvider(input devAnswerTTSInput) string {
	if value := strings.TrimSpace(input.ReviewProvider); value != "" {
		return value
	}
	return strings.TrimSpace(input.AgentProvider)
}

func effectiveReviewModel(input devAnswerTTSInput) string {
	if value := strings.TrimSpace(input.ReviewModel); value != "" {
		return value
	}
	return strings.TrimSpace(input.AgentModel)
}

func evaluateDevContinuity(
	ctx context.Context,
	agent *agentgateway.Gateway,
	input devAnswerTTSInput,
	plan devInteractionPlan,
	systemPrompt string,
) (devContinuityQuality, int64, error) {
	response, err := agent.Complete(ctx, agentgateway.Request{
		Provider: effectiveReviewProvider(input),
		Model:    effectiveReviewModel(input),
		Messages: []agentgateway.Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: devContinuityQualityPrompt(plan, input)},
		},
		MaxTokens:      700,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return devContinuityQuality{}, 0, err
	}
	quality, err := decodeDevContinuityQuality(response.Text)
	if err != nil {
		return devContinuityQuality{}, response.LatencyMS, err
	}
	return quality, response.LatencyMS, nil
}

func devContinuityQualityPrompt(plan devInteractionPlan, input devAnswerTTSInput) string {
	target := devResumeTarget(plan, input)
	payload, _ := json.Marshal(map[string]any{
		"question":              input.Question,
		"prompt_directives":     input.PromptDirectives,
		"mainline_before_stop":  input.CurrentMainline,
		"entry_mode":            plan.EntryMode,
		"entry_lead":            plan.EntryLead,
		"generated_reply_core":  plan.ReplyCore,
		"generated_resume_tail": plan.ResumeTail,
		"generated_full_text":   devInteractionFinalText(plan),
		"resume_mode":           plan.ResumeMode,
		"skip_units":            plan.SkipUnits,
		"resume_unit":           plan.ResumeUnit,
		"resume_target":         target,
		"continuous_playback":   strings.TrimSpace(input.CurrentMainline + " " + devInteractionFinalText(plan) + " " + target.Text),
	})
	return "本次接续质检动态数据：\n" + string(payload)
}

func devContinuityRepairPrompt(plan devInteractionPlan, input devAnswerTTSInput, quality devContinuityQuality, targetUnits int) string {
	target := devResumeTarget(plan, input)
	payload, _ := json.Marshal(map[string]any{
		"quality_score":        quality.Score,
		"quality_summary":      quality.Summary,
		"quality_issues":       quality.Issues,
		"quality_fatal":        quality.Fatal,
		"quality_fatal_reason": quality.FatalReason,
		"entry_mode":           plan.EntryMode,
		"entry_lead":           plan.EntryLead,
		"reply_core":           plan.ReplyCore,
		"resume_tail":          plan.ResumeTail,
		"covered_topics":       plan.CoveredTopics,
		"covered_fact_ids":     plan.CoveredFactIDs,
		"skip_units":           plan.SkipUnits,
		"resume_unit":          plan.ResumeUnit,
		"resume_mode":          plan.ResumeMode,
		"resume_target":        target,
		"target_text_units":    targetUnits,
		"target_seconds":       input.TargetSeconds,
		"tts_rate":             input.TTSRate,
		"mainline_before_stop": input.CurrentMainline,
	})
	return "本次接续修复动态数据：\n" + string(payload)
}
