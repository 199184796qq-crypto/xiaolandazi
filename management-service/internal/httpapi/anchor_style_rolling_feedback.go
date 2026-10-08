package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorstylescore"
)

type anchorStyleRollingFeedbackResponse struct {
	Status            string                         `json:"status"`
	Prompt            string                         `json:"prompt"`
	SelectedAddresses []string                       `json:"selected_addresses"`
	Report            anchorstylescore.RollingReport `json:"report"`
	Request           anchorStyleWorkbenchTrace      `json:"request"`
	RawResponse       string                         `json:"raw_response"`
	Error             string                         `json:"error,omitempty"`
	Warnings          []string                       `json:"warnings,omitempty"`
	CorrectionSources map[string]string              `json:"correction_sources,omitempty"`
	Model             string                         `json:"model"`
	Provider          string                         `json:"provider"`
	ProfileID         string                         `json:"profile_id"`
	LatencyMS         int64                          `json:"latency_ms"`
	InputTokens       int64                          `json:"input_tokens"`
	OutputTokens      int64                          `json:"output_tokens"`
	PromptSource      string                         `json:"prompt_source"`
	StyleAssetID      int64                          `json:"style_asset_id"`
	ScorecardAssetID  int64                          `json:"scorecard_asset_id"`
}

func normalizeRollingFeedback(raw string, allowed []string, issues []anchorstylescore.RollingIssue) (string, []string, error) {
	prompt, addresses, _, _, err := normalizeRollingFeedbackDetailed(raw, allowed, issues)
	return prompt, addresses, err
}

func rollingExpectedCategories(issues []anchorstylescore.RollingIssue) []string {
	result := []string{}
	for _, category := range []string{"opening", "style", "ending"} {
		for _, issue := range issues {
			if issue.Category == category {
				result = append(result, category)
				break
			}
		}
	}
	return result
}

func canonicalRollingCategory(category string) string {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "opening", "opening_rotation", "开头", "开头轮换":
		return "opening"
	case "style", "style_density", "style_burst", "风格", "主播风格", "风格校正", "主播风格校正":
		return "style"
	case "ending", "ending_rotation", "结尾", "结尾轮换":
		return "ending"
	default:
		return ""
	}
}

func normalizeRollingFeedbackDetailed(raw string, allowed []string, issues []anchorstylescore.RollingIssue) (string, []string, []string, map[string]string, error) {
	var output struct {
		Corrections []struct {
			Category    string `json:"category"`
			Instruction string `json:"instruction"`
		} `json:"corrections"`
		Addresses []string `json:"selected_addresses"`
	}
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(raw)), &output); err != nil {
		return "", nil, nil, nil, fmt.Errorf("回灌模型未返回合法JSON")
	}
	expected := map[string]bool{}
	for _, issue := range issues {
		expected[issue.Category] = true
	}
	if len(expected) == 0 {
		return "", []string{}, nil, map[string]string{}, nil
	}
	warnings := []string{}
	accepted := map[string]string{}
	seen := map[string]map[string]bool{}
	overflow := map[string]bool{}
	sources := map[string]string{}
	for _, correction := range output.Corrections {
		category := canonicalRollingCategory(correction.Category)
		if !expected[category] {
			warnings = append(warnings, "已过滤未触发类别："+correction.Category)
			continue
		}
		instruction := strings.TrimSpace(correction.Instruction)
		if n := utf8.RuneCountInString(instruction); n == 0 || n > 110 {
			warnings = append(warnings, "已过滤空白或超长指令："+category)
			continue
		}
		tooStrong := false
		for _, word := range []string{"大幅", "猛补", "猛减", "补足上一段"} {
			if strings.Contains(instruction, word) {
				tooStrong = true
				break
			}
		}
		if tooStrong {
			warnings = append(warnings, "已过滤过猛纠偏："+category)
			continue
		}
		if seen[category] == nil {
			seen[category] = map[string]bool{}
		}
		if seen[category][instruction] {
			warnings = append(warnings, "已去重相同指令："+category)
			continue
		}
		seen[category][instruction] = true
		if overflow[category] {
			continue
		}
		merged := instruction
		if accepted[category] != "" {
			merged = accepted[category] + "\n" + instruction
		}
		// A model may split several issues of one category into separate entries.
		// Preserve each distinct instruction instead of discarding all but the first.
		if utf8.RuneCountInString(merged) > 330 {
			overflow[category] = true
			delete(accepted, category)
			delete(sources, category)
			warnings = append(warnings, "同类指令合并超长，改用对应模板："+category)
			continue
		}
		if accepted[category] != "" {
			warnings = append(warnings, "已合并同类不同指令："+category)
		}
		accepted[category] = merged
		sources[category] = "model"
	}
	for _, address := range output.Addresses {
		valid := false
		for _, candidate := range allowed {
			if address == candidate {
				valid = true
				break
			}
		}
		if !valid {
			return "", nil, warnings, sources, fmt.Errorf("回灌模型选择了风格提示词之外的称呼：%s", address)
		}
	}
	if output.Addresses == nil {
		output.Addresses = []string{}
	}
	categories := rollingExpectedCategories(issues)
	var fallbacks map[string]string
	for _, category := range categories {
		if accepted[category] != "" {
			continue
		}
		if fallbacks == nil {
			md, _, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ROLLING_FALLBACK_PROMPT_PATH", "configs/prompts/anchor-style/rolling-feedback-fallback.md", "三项回灌兜底模板")
			if err != nil {
				return "", nil, warnings, sources, err
			}
			_, tail, ok := strings.Cut(md, "```json")
			body, _, closed := strings.Cut(tail, "```")
			if !ok || !closed || json.Unmarshal([]byte(strings.TrimSpace(body)), &fallbacks) != nil {
				return "", nil, warnings, sources, fmt.Errorf("三项回灌兜底模板无效")
			}
		}
		metrics := []string{}
		for _, issue := range issues {
			if issue.Category != category {
				continue
			}
			name, _, _ := strings.Cut(issue.Actual, "：")
			if issue.Key == "style_burst" {
				name = "语气词分布"
			}
			metrics = append(metrics, name)
		}
		instruction := strings.ReplaceAll(fallbacks[category], "{{metrics}}", strings.Join(metrics, "、"))
		if n := utf8.RuneCountInString(instruction); n == 0 || n > 110 {
			return "", nil, warnings, sources, fmt.Errorf("三项回灌兜底指令无效")
		}
		accepted[category] = instruction
		sources[category] = "program_fallback"
		warnings = append(warnings, "模型漏项或指令无效，已用对应模板补齐："+category)
	}
	parts := []string{}
	for _, category := range categories {
		parts = append(parts, accepted[category])
	}
	return strings.Join(parts, "\n"), output.Addresses, warnings, sources, nil
}

func (s *Server) anchorStyleWorkbenchRollingFeedback(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID    int64    `json:"tenant_id,omitempty"`
		ProfileID   string   `json:"feedback_profile_id"`
		StyleID     int64    `json:"meta_prompt_asset_id"`
		ScorecardID int64    `json:"scorecard_asset_id"`
		Texts       []string `json:"recent_texts"`
		StartIndex  int      `json:"start_index"`
	}
	if readJSON(w, r, &input) != nil || len(input.Texts) < 1 || len(input.Texts) > 5 || input.StartIndex < 1 || strings.TrimSpace(input.ProfileID) == "" {
		writeError(w, 400, "回灌须包含已选择模型、1到5段原文及有效窗口起点")
		return
	}
	for _, text := range input.Texts {
		if n := utf8.RuneCountInString(strings.TrimSpace(text)); n == 0 || n > 10000 {
			writeError(w, 400, "每段回灌原文须为1到10000字符")
			return
		}
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if err != nil || meta.Status != "confirmed" || meta.ID != input.StyleID {
		writeError(w, 409, "当前主播风格已变化或未确认")
		return
	}
	card, err := s.store.GetActiveLiveAnchorStyleScorecard(r.Context(), tenantID, planID)
	if err != nil || card.Status != "confirmed" || card.ID != input.ScorecardID || card.MetaPromptAssetID != meta.ID {
		writeError(w, 409, "当前评分基准已变化或未确认")
		return
	}
	b, err := anchorstylescore.Parse(card.EffectiveScorecardJSON())
	if err != nil {
		writeError(w, 409, "评分基准无效")
		return
	}
	profile, err := s.store.ResolveSpeechModel(r.Context(), input.ProfileID)
	if err != nil || profile == nil || !profile.Enabled || strings.TrimSpace(profile.APIKey) == "" {
		writeError(w, 400, "请选择可用的回灌模型，不能静默换模型")
		return
	}
	response := anchorStyleRollingFeedbackResponse{Status: "no_issue", SelectedAddresses: []string{}, ProfileID: input.ProfileID, StyleAssetID: meta.ID, ScorecardAssetID: card.ID, Report: anchorstylescore.AnalyzeRolling(input.Texts, input.StartIndex, b, meta.EffectiveMetaPromptMD())}
	if len(response.Report.Issues) == 0 {
		writeJSON(w, 200, response)
		return
	}
	md, source, err := loadReplaceablePromptAsset("ANCHOR_STYLE_ROLLING_FEEDBACK_PROMPT_PATH", "configs/prompts/anchor-style/rolling-feedback-generation.md", "滑动窗口三项回灌提示词")
	if err != nil {
		response.Status = "model_error"
		response.Error = err.Error()
		writeJSON(w, 200, response)
		return
	}
	data, _ := json.Marshal(map[string]any{"style_meta_prompt": meta.EffectiveMetaPromptMD(), "report": response.Report, "expected_categories": rollingExpectedCategories(response.Report.Issues), "recent_texts": input.Texts})
	messages := []agentgateway.Message{{Role: "system", Content: md}, {Role: "user", Content: string(data)}}
	response.Request = traceAnchorStyleWorkbenchMessages(messages)
	response.PromptSource = source
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), input.ProfileID, "rolling_style_feedback", agentgateway.ResponseJSON, messages, 1000, false)
	response.Request = traceAnchorStyleWorkbenchMessages(actual)
	response.RawResponse = result.Text
	response.Model = result.Model
	response.Provider = result.Provider
	response.LatencyMS = result.LatencyMS
	response.InputTokens = result.InputTokens
	response.OutputTokens = result.OutputTokens
	if err != nil {
		response.Status = "model_error"
		response.Error = err.Error()
		writeJSON(w, 200, response)
		return
	}
	// A late completion cannot be applied to a replaced style or scorecard.
	currentMeta, metaErr := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	currentCard, cardErr := s.store.GetActiveLiveAnchorStyleScorecard(r.Context(), tenantID, planID)
	if metaErr != nil || cardErr != nil || currentMeta.ID != meta.ID || currentCard.ID != card.ID || currentMeta.Status != "confirmed" || currentCard.Status != "confirmed" {
		response.Status = "model_error"
		response.Error = "生成回灌期间风格或基准已变化"
		writeJSON(w, 200, response)
		return
	}
	response.Prompt, response.SelectedAddresses, response.Warnings, response.CorrectionSources, err = normalizeRollingFeedbackDetailed(result.Text, response.Report.AllowedAddresses, response.Report.Issues)
	if err != nil {
		response.Status = "invalid_response"
		response.Error = err.Error()
	} else {
		response.Status = "ready"
	}
	writeJSON(w, 200, response)
}
