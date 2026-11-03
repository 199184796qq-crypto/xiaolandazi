package httpapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
)

// planScriptRewrite 用方案专属的改写元提示词，把一条样本话术改写成新话术。
//
// 流程：读方案保存时生成的改写提示词母版 → 把本次样本填进 {{样本话术}}
// → 调模型 → 解析「话术骨架 / 话术内容」两个字段返回。
type planScriptRewriteInput struct {
	Text string `json:"text"`
}

type planScriptRewriteOutput struct {
	Skeleton string `json:"skeleton"`
	Script   string `json:"script"`
	Model    string `json:"model,omitempty"`
	Usage    map[string]int64 `json:"usage,omitempty"`
}

func (s *Server) planScriptRewrite(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, 0, false)
	if !ok {
		return
	}
	var input planScriptRewriteInput
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "改写请求格式错误")
		return
	}
	sample := strings.TrimSpace(input.Text)
	if runes := utf8.RuneCountInString(sample); runes < 100 || runes > 8000 {
		writeError(w, http.StatusBadRequest, "样本话术长度需在 100–8000 字之间")
		return
	}
	if _, err := s.store.GetLiveAgentPlan(r.Context(), tenantID, planID); err != nil {
		writeError(w, http.StatusNotFound, "方案不存在或无权查看")
		return
	}
	document, err := s.store.GetLiveAgentPlanPromptDocument(r.Context(), tenantID, planID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取方案提示词失败")
		return
	}
	promptMD := strings.TrimSpace(document.RewritePromptMD)
	if promptMD == "" {
		// 老方案还没生成过改写提示词：用当前分区现算一份，保证能用。
		promptMD, err = renderPlanRewritePrompt(document.Sections)
		if err != nil || strings.TrimSpace(promptMD) == "" {
			writeError(w, http.StatusConflict, "当前方案还没有可用的改写提示词，请先保存一次方案提示词")
			return
		}
	}
	prompt := strings.ReplaceAll(promptMD, "{{样本话术}}", sample)
	result, err := s.speechGateway().Complete(r.Context(), agentgateway.Request{
		Stage:      "script_rewrite",
		Messages:   []agentgateway.Message{{Role: "user", Content: prompt}},
		MaxTokens:  2400,
		Timeout:    180 * time.Second,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "改写请求失败，请稍后重试")
		return
	}
	skeleton, script := parseRewriteOutput(result.Text)
	if strings.TrimSpace(script) == "" {
		writeError(w, http.StatusBadGateway, "改写结果没有返回正文，请重试")
		return
	}
	writeJSON(w, http.StatusOK, planScriptRewriteOutput{
		Skeleton: skeleton,
		Script:   script,
		Model:    result.Model,
		Usage: map[string]int64{
			"prompt_tokens":     result.InputTokens,
			"completion_tokens": result.OutputTokens,
			"total_tokens":      result.TotalTokens,
		},
	})
}

// parseRewriteOutput 按约定的 KV 格式切出骨架和正文。
//
// 模型偶尔会带引号或多余空行，这里只做最小清洗，不做自由发挥。
func parseRewriteOutput(raw string) (skeleton, script string) {
	text := strings.ReplaceAll(raw, "\r\n", "\n")
	if idx := strings.Index(text, "话术内容"); idx >= 0 {
		head := text[:idx]
		body := text[idx:]
		if start, width, ok := findColon(body); ok {
			body = body[start+width:]
		}
		script = strings.TrimSpace(body)
		if idx := strings.Index(head, "话术骨架"); idx >= 0 {
			rest := head[idx:]
			if start, width, ok := findColon(rest); ok {
				rest = rest[start+width:]
			}
			skeleton = strings.TrimSpace(rest)
		}
		return skeleton, script
	}
	return "", strings.TrimSpace(text)
}

// findColon 找到第一个中文冒号或英文冒号，并返回它的字节长度。
//
// 中文冒号是 3 字节，直接 +1 会切坏字符，必须按字节宽度跳过。
func findColon(text string) (int, int, bool) {
	for i, r := range text {
		switch r {
		case '：':
			return i, len("："), true
		case ':':
			return i, 1, true
		}
	}
	return 0, 0, false
}