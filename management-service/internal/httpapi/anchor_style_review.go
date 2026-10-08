package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	appdb "livecompanion/management/internal/db"
)

type anchorStyleReviewDimension struct {
	Name      string   `json:"name"`
	Score     int      `json:"score"`
	Evidence  []string `json:"evidence"`
	Diagnosis string   `json:"diagnosis"`
}

type anchorStyleReviewResult struct {
	OverallScore   int                          `json:"overall_score"`
	Dimensions     []anchorStyleReviewDimension `json:"dimensions"`
	Strengths      []string                     `json:"strengths"`
	Deviations     []string                     `json:"deviations"`
	FeedbackPrompt string                       `json:"feedback_prompt"`
}

type anchorStyleReviewResponse struct {
	Review       anchorStyleReviewResult   `json:"review"`
	FastStats    anchorStyleWorkbenchStats `json:"fast_stats"`
	Request      anchorStyleWorkbenchTrace `json:"request"`
	RawResponse  string                    `json:"raw_response"`
	PromptSource string                    `json:"prompt_source"`
	Provider     string                    `json:"provider"`
	Model        string                    `json:"model"`
	LatencyMS    int64                     `json:"latency_ms"`
	InputTokens  int64                     `json:"input_tokens"`
	OutputTokens int64                     `json:"output_tokens"`
	WindowCount  int                       `json:"window_count"`
	WindowChars  int                       `json:"window_chars"`
}

func (s *Server) anchorStyleWorkbenchStyleReview(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	planID, ok := liveAgentPlanPathID(w, r)
	if !ok {
		return
	}
	var input struct {
		TenantID          int64                `json:"tenant_id,omitempty"`
		SupervisorProfile string               `json:"supervisor_profile_id"`
		GeneratedText     string               `json:"generated_text"`
		RecentTexts       []string             `json:"recent_texts"`
		Task              anchorStyleWheelTask `json:"task"`
	}
	if readJSON(w, r, &input) != nil {
		writeError(w, http.StatusBadRequest, "风格评分请求格式错误")
		return
	}
	input.GeneratedText = strings.TrimSpace(input.GeneratedText)
	if input.GeneratedText == "" || utf8.RuneCountInString(input.GeneratedText) > 10000 {
		writeError(w, http.StatusBadRequest, "待评分话术须为1到10000字")
		return
	}
	if len(input.RecentTexts) > 5 {
		input.RecentTexts = input.RecentTexts[len(input.RecentTexts)-5:]
	}
	recent := make([]string, 0, len(input.RecentTexts)+1)
	for _, item := range input.RecentTexts {
		item = strings.TrimSpace(item)
		if item != "" && utf8.RuneCountInString(item) <= 10000 {
			recent = append(recent, item)
		}
	}
	if len(recent) == 0 || recent[len(recent)-1] != input.GeneratedText {
		recent = append(recent, input.GeneratedText)
	}
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	tenantID, ok := s.resolveLiveAgentPlanTenant(w, r, actor, input.TenantID, false)
	if !ok {
		return
	}
	meta, err := s.store.GetActiveLiveAnchorStyleMetaPrompt(r.Context(), tenantID, planID)
	if errors.Is(err, appdb.ErrLiveAnchorStyleMetaPromptNotFound) || (err == nil && meta.Status != "confirmed") {
		writeError(w, http.StatusConflict, "请先确认并保存当前方案的主播风格元提示词")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取主播风格元提示词失败")
		return
	}
	var monitor anchorStyleWorkbenchMonitor
	if strings.TrimSpace(meta.MonitorJSON) != "" {
		_ = json.Unmarshal([]byte(meta.MonitorJSON), &monitor)
	}
	windowText := strings.Join(recent, "\n")
	fastStats := evaluateAnchorStyleWorkbench(monitor, windowText)
	reviewPrompt, promptSource, err := loadAnchorStyleReviewPrompt()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	statsJSON, _ := json.MarshalIndent(fastStats, "", "  ")
	userPrompt := renderAnchorStyleReviewRequest(meta.EffectiveMetaPromptMD(), string(statsJSON), input.Task, recent, input.GeneratedText)
	messages := []agentgateway.Message{
		{Role: "system", Content: reviewPrompt},
		{Role: "user", Content: userPrompt},
	}
	result, actual, err := s.completeAnchorStyleWorkbench(r.Context(), input.SupervisorProfile, "style_feedback_review", agentgateway.ResponseJSON, messages, 2400, false)
	if err != nil {
		writeError(w, http.StatusBadGateway, "风格监督评分失败："+err.Error())
		return
	}
	raw := strings.TrimSpace(result.Text)
	review, err := normalizeAnchorStyleReviewJSON(raw)
	if err != nil {
		writeError(w, http.StatusBadGateway, "风格监督返回格式无效："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, anchorStyleReviewResponse{
		Review: review, FastStats: fastStats, Request: traceAnchorStyleWorkbenchMessages(actual),
		RawResponse: raw, PromptSource: promptSource, Provider: result.Provider, Model: result.Model,
		LatencyMS: result.LatencyMS, InputTokens: result.InputTokens, OutputTokens: result.OutputTokens,
		WindowCount: len(recent), WindowChars: utf8.RuneCountInString(windowText),
	})
}

func renderAnchorStyleReviewRequest(metaPrompt, fastStatsJSON string, task anchorStyleWheelTask, recent []string, current string) string {
	window := make([]string, 0, len(recent))
	for index, item := range recent {
		window = append(window, fmt.Sprintf("## 第%d段\n%s", index+1, item))
	}
	return fmt.Sprintf(`# 已确认的主播风格元提示词
%s

# 本轮卖点与角度（仅用于定位，不评价内容事实）
%s × %s

# 程序快速统计（仅作辅助，不得机械照抄结论）
%s

# 最近口播滑动窗口
%s

# 本轮最新话术（评分证据优先引用本段）
%s

请严格只评腔调风格，返回约定JSON。`, strings.TrimSpace(metaPrompt), task.SellingPoint, task.Angle, fastStatsJSON, strings.Join(window, "\n\n"), current)
}

func normalizeAnchorStyleReviewJSON(raw string) (anchorStyleReviewResult, error) {
	raw = strings.TrimSpace(stripPolicyJSONFence(raw))
	var result anchorStyleReviewResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return result, errors.New("不是合法JSON")
	}
	if result.OverallScore < 0 || result.OverallScore > 100 {
		return result, errors.New("overall_score须为0到100")
	}
	if len(result.Dimensions) == 0 || len(result.Dimensions) > 10 {
		return result, errors.New("dimensions数量无效")
	}
	for index := range result.Dimensions {
		item := &result.Dimensions[index]
		item.Name = strings.TrimSpace(item.Name)
		item.Diagnosis = strings.TrimSpace(item.Diagnosis)
		if item.Name == "" || item.Score < 0 || item.Score > 100 {
			return result, errors.New("评分维度名称或分数无效")
		}
		item.Evidence = cleanAnchorStyleReviewStrings(item.Evidence, 3, 60)
	}
	result.Strengths = cleanAnchorStyleReviewStrings(result.Strengths, 3, 120)
	result.Deviations = cleanAnchorStyleReviewStrings(result.Deviations, 3, 120)
	result.FeedbackPrompt = strings.TrimSpace(result.FeedbackPrompt)
	if result.FeedbackPrompt == "" || utf8.RuneCountInString(result.FeedbackPrompt) > 300 {
		return result, errors.New("feedback_prompt为空或超过300字")
	}
	return result, nil
}

func cleanAnchorStyleReviewStrings(items []string, limit, runeLimit int) []string {
	result := make([]string, 0, min(len(items), limit))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if utf8.RuneCountInString(item) > runeLimit {
			item = string([]rune(item)[:runeLimit])
		}
		result = append(result, item)
		if len(result) >= limit {
			break
		}
	}
	return result
}
