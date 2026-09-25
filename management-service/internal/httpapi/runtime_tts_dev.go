package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/ttsgateway"
)

const devSpeechUnitsPerSecondAt1x = 5.6

func plannedSpeechUnits(targetSeconds int, rate float64) int {
	if rate <= 0 {
		rate = 1
	}
	return int(float64(targetSeconds)*devSpeechUnitsPerSecondAt1x*rate + 0.5)
}

type devAnswerTTSInput struct {
	Question           string                 `json:"question"`
	TargetSeconds      int                    `json:"target_seconds"`
	AgentProvider      string                 `json:"agent_provider"`
	AgentModel         string                 `json:"agent_model"`
	ReviewProvider     string                 `json:"review_provider,omitempty"`
	ReviewModel        string                 `json:"review_model,omitempty"`
	TTSProvider        string                 `json:"tts_provider"`
	TTSModel           string                 `json:"tts_model"`
	VoiceID            string                 `json:"voice_id"`
	TTSRate            float64                `json:"tts_rate"`
	DirectorProgress   string                 `json:"director_progress,omitempty"`
	DirectorAtmosphere string                 `json:"director_atmosphere,omitempty"`
	HumanizationKind   string                 `json:"humanization_kind,omitempty"`
	ResumeModeHint     string                 `json:"resume_mode_hint,omitempty"`
	CurrentMainline    string                 `json:"current_mainline,omitempty"`
	NextMainlineUnits  []devMainlineUnitInput `json:"next_mainline_units,omitempty"`
	TopTopics          []string               `json:"top_topics,omitempty"`
	PromptDirectives   []string               `json:"prompt_directives,omitempty"`
}

type devMainlineUnitInput struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Topics  []string `json:"topics,omitempty"`
	StartMS int      `json:"start_ms,omitempty"`
}

type devAnswerTTSOutput struct {
	Text              string               `json:"text"`
	EntryMode         string               `json:"entry_mode"`
	EntryLead         string               `json:"entry_lead"`
	ReplyCore         string               `json:"reply_core"`
	ResumeTail        string               `json:"resume_tail"`
	FinalText         string               `json:"final_text"`
	CoveredTopics     []string             `json:"covered_topics"`
	CoveredFactIDs    []string             `json:"covered_fact_ids"`
	SkipUnits         []string             `json:"skip_units"`
	ResumeUnit        string               `json:"resume_unit"`
	ResumeMode        string               `json:"resume_mode"`
	TextDigest        string               `json:"text_digest"`
	BridgeDigest      string               `json:"bridge_digest"`
	TargetSeconds     int                  `json:"target_seconds"`
	EstimatedSeconds  float64              `json:"estimated_seconds"`
	AgentProvider     string               `json:"agent_provider"`
	AgentModel        string               `json:"agent_model"`
	AgentLatencyMS    int64                `json:"agent_latency_ms"`
	ReviewProvider    string               `json:"review_provider"`
	ReviewModel       string               `json:"review_model"`
	ReviewLatencyMS   int64                `json:"review_latency_ms"`
	TTSProvider       string               `json:"tts_provider"`
	TTSModel          string               `json:"tts_model"`
	VoiceID           string               `json:"voice_id"`
	TTSRate           float64              `json:"tts_rate"`
	AudioURL          string               `json:"audio_url"`
	TTSLatencyMS      int64                `json:"tts_latency_ms"`
	ContinuityQuality devContinuityQuality `json:"continuity_quality"`
	QualityAttempts   int                  `json:"quality_attempts"`
}

func spokenUnitCount(text string) int {
	count := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			count++
		}
	}
	return count
}

func devAnswerPrompt(question string, targetSeconds, targetUnits int) string {
	minUnits := int(float64(targetUnits) * 0.90)
	maxUnits := int(float64(targetUnits) * 1.10)
	return "你是直播口播回答策划器。当前是纯链路测试，不加载行业策略、用户策略或固定直播话术。\n" +
		"请直接、自然地回答用户问题，内容可以自由发挥，但不要编造具体事实。\n" +
		"目标口播时长约 " + itoa(targetSeconds) + " 秒。按正常中文口播速度控制正文约 " +
		itoa(targetUnits) + " 个有效文字单位，建议范围 " + itoa(minUnits) + " 到 " + itoa(maxUnits) + "。\n" +
		"只输出最终可直接拿去做 TTS 的口播正文，不要标题、不要解释、不要标注时长、不要 Markdown。\n" +
		"句子要自然完整，不要为了凑长度机械重复。\n\n用户问题：" + question
}

func refineDurationPrompt(text string, targetSeconds, targetUnits int) string {
	minUnits := int(float64(targetUnits) * 0.92)
	maxUnits := int(float64(targetUnits) * 1.08)
	return "把下面这段口播在保持核心意思的前提下调整长度，使正常中文口播约 " +
		itoa(targetSeconds) + " 秒，有效文字单位控制在 " + itoa(minUnits) + " 到 " + itoa(maxUnits) +
		"。只输出调整后的口播正文，不要解释，不要标题，不要 Markdown。\n\n原文：" + text
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var buf [24]byte
	i := len(buf)
	for value > 0 {
		i--
		buf[i] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (s *Server) devRuntimeAnswerTTS(w http.ResponseWriter, r *http.Request) {
	if s.env != "development" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	var input devAnswerTTSInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.Question = strings.TrimSpace(input.Question)
	input.AgentProvider = strings.TrimSpace(input.AgentProvider)
	input.AgentModel = strings.TrimSpace(input.AgentModel)
	input.ReviewProvider = strings.TrimSpace(input.ReviewProvider)
	input.ReviewModel = strings.TrimSpace(input.ReviewModel)
	input.TTSProvider = strings.TrimSpace(input.TTSProvider)
	input.TTSModel = strings.TrimSpace(input.TTSModel)
	input.VoiceID = strings.TrimSpace(input.VoiceID)
	if input.TTSRate == 0 {
		input.TTSRate = 1.0
	}
	input.DirectorProgress = strings.TrimSpace(input.DirectorProgress)
	input.DirectorAtmosphere = strings.TrimSpace(input.DirectorAtmosphere)
	input.HumanizationKind = strings.TrimSpace(input.HumanizationKind)
	input.ResumeModeHint = strings.TrimSpace(input.ResumeModeHint)
	input.CurrentMainline = strings.TrimSpace(input.CurrentMainline)
	input.NextMainlineUnits = normalizeDevMainlineUnits(input.NextMainlineUnits, 16)
	input.TopTopics = normalizeUniqueStrings(input.TopTopics, 16)
	input.PromptDirectives = normalizeUniqueStrings(input.PromptDirectives, 24)
	if input.Question == "" || utf8.RuneCountInString(input.Question) > 2000 {
		writeError(w, http.StatusBadRequest, "请输入 1 到 2000 字的问题")
		return
	}
	if input.TargetSeconds < 3 || input.TargetSeconds > 60 {
		writeError(w, http.StatusBadRequest, "target_seconds 只允许 3 到 60 秒")
		return
	}
	if input.VoiceID == "" {
		writeError(w, http.StatusBadRequest, "voice_id is required")
		return
	}
	if input.TTSRate < 0.5 || input.TTSRate > 2.0 {
		writeError(w, http.StatusBadRequest, "tts_rate 只允许 0.5 到 2.0")
		return
	}

	// The target duration covers reply_core + resume_tail together.
	// audio-service measures the real WAV duration again before scheduling.
	// This is an empirical text-length planning heuristic for the current
	// cloned voice. audio-service still measures the real WAV duration after
	// synthesis. Production calibration should eventually be stored per
	// voice/profile instead of using one global coefficient.
	targetUnits := plannedSpeechUnits(input.TargetSeconds, input.TTSRate)
	agent := agentgateway.NewFromEnv()
	agentResult, err := agent.Complete(r.Context(), agentgateway.Request{
		Provider: input.AgentProvider,
		Model:    input.AgentModel,
		Messages: []agentgateway.Message{
			{Role: "system", Content: "你负责生成结构化的直播互动话术计划，回答正文和语义桥接必须一次规划完成。"},
			{Role: "user", Content: devInteractionPrompt(input, targetUnits)},
		},
		MaxTokens:      1200,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "Agent 生成失败："+err.Error())
		return
	}
	plan, err := decodeDevInteractionPlan(agentResult.Text)
	if err != nil {
		writeError(w, http.StatusBadGateway, "Agent 结构化计划解析失败："+err.Error())
		return
	}
	plan, resumeChanged := resolveDevInteractionResume(plan, input)
	needsResumeCorrection := resumeChanged || devResumeTailOverlapsTarget(plan, input)
	if needsResumeCorrection {
		corrected, correctionErr := agent.Complete(r.Context(), agentgateway.Request{
			Provider: input.AgentProvider,
			Model:    input.AgentModel,
			Messages: []agentgateway.Message{
				{Role: "system", Content: "你只负责修正直播互动后的回归桥接，使桥接准确接到程序已经确定的主线语义段。不得改变回答事实。"},
				{Role: "user", Content: devResumeCorrectionPrompt(plan, input)},
			},
			MaxTokens:      900,
			EnableThinking: false,
			ResponseFormat: agentgateway.ResponseJSON,
			Timeout:        30 * time.Second,
		})
		if correctionErr == nil {
			if correctedPlan, decodeErr := decodeDevInteractionPlan(corrected.Text); decodeErr == nil {
				correctedPlan.EntryMode = plan.EntryMode
				correctedPlan.EntryLead = plan.EntryLead
				correctedPlan.CoveredTopics = plan.CoveredTopics
				correctedPlan.CoveredFactIDs = plan.CoveredFactIDs
				correctedPlan.SkipUnits = plan.SkipUnits
				correctedPlan.ResumeUnit = plan.ResumeUnit
				correctedPlan.ResumeMode = plan.ResumeMode
				plan = correctedPlan
				agentResult.LatencyMS += corrected.LatencyMS
			}
		}
	}
	plan, _ = sanitizeDevResumeTail(plan, input)

	finalText := devInteractionFinalText(plan)
	units := spokenUnitCount(finalText)
	minUnits := int(float64(targetUnits) * 0.88)
	maxUnits := int(float64(targetUnits) * 1.12)
	for attempt := 0; attempt < 4 && (units < minUnits || units > maxUnits); attempt++ {
		refined, refineErr := agent.Complete(r.Context(), agentgateway.Request{
			Provider: input.AgentProvider,
			Model:    input.AgentModel,
			Messages: []agentgateway.Message{
				{Role: "system", Content: "你只负责调整结构化直播互动计划的口播长度，必须严格达到给定文字数量范围，并保留回答和桥接结构。"},
				{Role: "user", Content: refineDevInteractionPrompt(plan, input, targetUnits, units)},
			},
			MaxTokens:      1600,
			EnableThinking: false,
			ResponseFormat: agentgateway.ResponseJSON,
			Timeout:        30 * time.Second,
		})
		if refineErr != nil {
			break
		}
		refinedPlan, decodeErr := decodeDevInteractionPlan(refined.Text)
		if decodeErr != nil {
			break
		}
		refinedPlan.CoveredTopics = plan.CoveredTopics
		refinedPlan.CoveredFactIDs = plan.CoveredFactIDs
		refinedPlan.SkipUnits = plan.SkipUnits
		refinedPlan.ResumeUnit = plan.ResumeUnit
		refinedPlan.ResumeMode = plan.ResumeMode
		plan = refinedPlan
		plan, _ = sanitizeDevResumeTail(plan, input)
		finalText = devInteractionFinalText(plan)
		units = spokenUnitCount(finalText)
		agentResult.LatencyMS += refined.LatencyMS
	}
	if units > targetUnits {
		plan = fitDevInteractionPlanMaxUnits(plan, targetUnits)
		plan, _ = sanitizeDevResumeTail(plan, input)
		finalText = devInteractionFinalText(plan)
		units = spokenUnitCount(finalText)
	}

	qualityAttempts := 1
	quality, qualityLatencyMS, qualityErr := evaluateDevContinuity(r.Context(), agent, input, plan)
	if qualityErr != nil {
		writeError(w, http.StatusBadGateway, "接续质量评估失败，已停止语音合成："+qualityErr.Error())
		return
	}
	reviewLatencyMS := qualityLatencyMS

	for repairAttempt := 0; repairAttempt < 2 && !devContinuityQualityPass(quality); repairAttempt++ {
		repaired, repairErr := agent.Complete(r.Context(), agentgateway.Request{
			Provider: input.AgentProvider,
			Model:    input.AgentModel,
			Messages: []agentgateway.Message{
				{Role: "system", Content: "你负责根据接续质检意见修正直播插播文案与切入策略。前接允许顺接、软接或有意硬接，不要求强行同主题；后接必须严格做到自然、不重复、不抢讲下一段。"},
				{Role: "user", Content: devContinuityRepairPrompt(plan, input, quality, targetUnits)},
			},
			MaxTokens:      1400,
			EnableThinking: false,
			ResponseFormat: agentgateway.ResponseJSON,
			Timeout:        30 * time.Second,
		})
		if repairErr != nil {
			break
		}
		repairedPlan, decodeErr := decodeDevInteractionPlan(repaired.Text)
		if decodeErr != nil {
			break
		}
		repairedPlan.CoveredTopics = plan.CoveredTopics
		repairedPlan.CoveredFactIDs = plan.CoveredFactIDs
		repairedPlan.SkipUnits = plan.SkipUnits
		repairedPlan.ResumeUnit = plan.ResumeUnit
		repairedPlan.ResumeMode = plan.ResumeMode
		plan = repairedPlan
		plan, _ = sanitizeDevResumeTail(plan, input)
		finalText = devInteractionFinalText(plan)
		units = spokenUnitCount(finalText)
		if units > targetUnits {
			plan = fitDevInteractionPlanMaxUnits(plan, targetUnits)
			plan, _ = sanitizeDevResumeTail(plan, input)
			finalText = devInteractionFinalText(plan)
			units = spokenUnitCount(finalText)
		}

		agentResult.LatencyMS += repaired.LatencyMS
		qualityAttempts++
		quality, qualityLatencyMS, qualityErr = evaluateDevContinuity(r.Context(), agent, input, plan)
		if qualityErr != nil {
			break
		}
		reviewLatencyMS += qualityLatencyMS
	}

	if qualityErr != nil {
		writeError(w, http.StatusBadGateway, "接续质量复评失败，已停止语音合成："+qualityErr.Error())
		return
	}
	if !devContinuityQualityPass(quality) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":              "接续质量未达到中高，已拦截语音合成",
			"entry_mode":         plan.EntryMode,
			"entry_lead":         plan.EntryLead,
			"reply_core":         plan.ReplyCore,
			"resume_tail":        plan.ResumeTail,
			"final_text":         finalText,
			"covered_topics":     plan.CoveredTopics,
			"skip_units":         plan.SkipUnits,
			"resume_unit":        plan.ResumeUnit,
			"resume_mode":        plan.ResumeMode,
			"continuity_quality": quality,
			"quality_attempts":   qualityAttempts,
		})
		return
	}

	ttsStarted := time.Now()
	ttsResult, err := ttsgateway.NewFromEnv().SynthesizeURL(r.Context(), ttsgateway.SynthesizeRequest{
		Provider: input.TTSProvider,
		Model:    input.TTSModel,
		VoiceID:  input.VoiceID,
		Text:     finalText,
		Rate:     input.TTSRate,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "TTS 生成失败："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, devAnswerTTSOutput{
		Text:              finalText,
		EntryMode:         plan.EntryMode,
		EntryLead:         plan.EntryLead,
		ReplyCore:         plan.ReplyCore,
		ResumeTail:        plan.ResumeTail,
		FinalText:         finalText,
		CoveredTopics:     plan.CoveredTopics,
		CoveredFactIDs:    plan.CoveredFactIDs,
		SkipUnits:         plan.SkipUnits,
		ResumeUnit:        plan.ResumeUnit,
		ResumeMode:        plan.ResumeMode,
		TextDigest:        devTextDigest(finalText),
		BridgeDigest:      devTextDigest(plan.ResumeTail),
		TargetSeconds:     input.TargetSeconds,
		EstimatedSeconds:  float64(units) / (devSpeechUnitsPerSecondAt1x * input.TTSRate),
		AgentProvider:     agentResult.Provider,
		AgentModel:        agentResult.Model,
		AgentLatencyMS:    agentResult.LatencyMS,
		ReviewProvider:    effectiveReviewProvider(input),
		ReviewModel:       effectiveReviewModel(input),
		ReviewLatencyMS:   reviewLatencyMS,
		TTSProvider:       ttsResult.Provider,
		TTSModel:          ttsResult.Model,
		VoiceID:           ttsResult.VoiceID,
		TTSRate:           ttsResult.Rate,
		AudioURL:          ttsResult.AudioURL,
		TTSLatencyMS:      time.Since(ttsStarted).Milliseconds(),
		ContinuityQuality: quality,
		QualityAttempts:   qualityAttempts,
	})
}
