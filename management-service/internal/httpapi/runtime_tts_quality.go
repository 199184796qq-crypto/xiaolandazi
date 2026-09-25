package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
) (devContinuityQuality, int64, error) {
	response, err := agent.Complete(ctx, agentgateway.Request{
		Provider: effectiveReviewProvider(input),
		Model:    effectiveReviewModel(input),
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你是直播口播接续的严格质检员，只负责验收，不替生成结果找理由。必须按两段连续播放后的真实听感评分。"},
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
	return "你是直播场控系统的最终评审 Agent。你不是生成文案，也不能替生成结果找理由。" +
		"prompt_directives 是研发测试中已经临时加载的人工确认规则/上下文线索；评审问题意图和事实时必须把它作为重要依据，不能忽略。" +
		"评审顺序必须严格按：第一，是否正确理解 question 的真实意图；第二，回答里的商品事实和承诺是否有上下文依据；第三，是否真正回答了问题；第四，才检查切入、回归、重复和整体听感。" +
		"如果明显误解问题意图，例如把口语/方言/多义词理解成另一个完全不同的意思，fatal=true；这种情况即使前后接续再自然也不得达到中高质量。" +
		"如果回答加入了上下文没有提供、又会影响用户判断的具体商品事实、价格、库存、物流承诺、规格、功效或售后承诺，也要 fatal=true。一般性的语气词或不改变事实的连接表达不属于 fatal。" +
		"请把 mainline_before_stop、generated_full_text、resume_target.text 当成主播连续说出的三段，判断连续播放后的真实听感。" +
		"切入侧比回归侧宽松：DIRECT=顺接，SOFT=软接，HARD=硬接。硬接本身不是扣分项，只要问题值得立即回答、听众能理解为什么此时插答即可；不要强求切入前后必须同主题。" +
		"按100分制评分：问题意图理解与回答正确性30分；事实有据与真实性20分；切入方式10分；回归自然度20分；重复/抢讲下一段15分；整体口语听感5分。" +
		"凡是 question、mainline_before_stop、resume_target 以及给出的真实上下文没有明确提供的具体业务事实，都不能因为常识自行补齐。" +
		"特别严格检查：插播末尾不能把 resume_target 的具体事实先讲一遍；如果回答已经覆盖后续主线，应确认 skip_units/resume_unit 后的组合不会再次重复。" +
		"fatal_reason 在 fatal=false 时留空。issues 用简短中文列出问题，没有问题返回空数组。summary 用一句中文说明最终判断。" +
		"只返回 JSON：score, fatal, fatal_reason, summary, issues。不要 Markdown。\n\n待验收内容：\n" + string(payload)
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
	return fmt.Sprintf(
		"上一版最终评审只有 %d 分，没有达到中高质量。请优先修正意图理解和事实问题，再根据 quality_issues 修正 reply_core、切入方式和 resume_tail，然后重新输出完整结构 JSON。"+
			"同时允许重新选择 entry_mode（DIRECT/SOFT/HARD）和重写 entry_lead；切入前侧可以比回归后侧宽松，重点是让听众理解为什么此时转去回答问题。"+
			"必须保留 covered_topics、covered_fact_ids、skip_units、resume_unit、resume_mode，不得改变程序已经确定的跳转。"+
			"修正后的 generated_full_text 紧接 resume_target.text 时必须自然，不能重复、复述或提前讲 resume_target 的具体信息。"+
			"如果直接接 resume_target 最自然，FUSION_SKIP/CROSS_RESUME 的 resume_tail 可以留空。"+
			"不要为了凑时长编造价格、包邮、库存、物流承诺、规格、功效等未提供事实。"+
			"总有效中文文字/数字单位尽量接近 %d。只返回 entry_mode, entry_lead, reply_core, resume_tail, covered_topics, covered_fact_ids, skip_units, resume_unit, resume_mode。\n\n%s",
		quality.Score,
		targetUnits,
		string(payload),
	)
}
