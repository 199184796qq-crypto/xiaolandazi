package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type liveAgentChatHistoryItem struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type liveAgentChatInput struct {
	Message          string                     `json:"message"`
	AnchorTranscript string                     `json:"anchor_transcript"`
	History          []liveAgentChatHistoryItem `json:"history"`
}

type liveAgentChatOutput struct {
	Reply     string `json:"reply"`
	Kind      string `json:"kind"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

func (s *Server) liveAgentChat(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	roomID, ok := pathID(w, r)
	if !ok {
		return
	}
	tenantID, ok := s.tenantForRoom(w, r, actor, roomID)
	if !ok {
		return
	}

	var input liveAgentChatInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "场控 Agent 请求格式错误")
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	input.AnchorTranscript = strings.TrimSpace(input.AnchorTranscript)
	if input.Message == "" {
		writeError(w, http.StatusBadRequest, "请输入要问场控 Agent 的内容")
		return
	}
	if utf8.RuneCountInString(input.Message) > 4000 {
		writeError(w, http.StatusBadRequest, "场控 Agent 单次输入不能超过 4000 字")
		return
	}
	if !actor.IsInternalStaff() && customerAgentRestrictedRequest(input.Message) {
		writeJSON(w, http.StatusOK, liveAgentChatOutput{
			Reply: clientAgentBoundaryReply,
			Kind:  "boundary",
		})
		return
	}
	if utf8.RuneCountInString(input.AnchorTranscript) > 4000 {
		input.AnchorTranscript = string([]rune(input.AnchorTranscript)[:4000])
	}
	if len(input.History) > 10 {
		input.History = input.History[len(input.History)-10:]
	}

	settings, err := s.store.GetLiveAgentSettings(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取场控 Agent 设置失败")
		return
	}

	industryCode, l1, l2, l3, err := s.store.LoadLivePolicyLayers(
		r.Context(), tenantID, roomID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间有效策略失败")
		return
	}
	effectivePolicy := policy.BuildEffective(industryCode, l1, l2, l3)
	effectivePolicyPrompt := effectivePolicy.PromptText
	if !actor.IsInternalStaff() {
		effectivePolicyPrompt = customerSafePolicyPrompt(effectivePolicy)
	}
	runtimeInstruction := s.store.AgentPromptValue(r.Context(), "policy.runtime.execution", "")
	if strings.TrimSpace(runtimeInstruction) != "" {
		effectivePolicyPrompt = strings.TrimSpace(runtimeInstruction + "\n\n" + effectivePolicyPrompt)
	}

	if reply, matched := localLiveAgentAnswer(input.Message); matched {
		writeJSON(w, http.StatusOK, liveAgentChatOutput{
			Reply: reply,
			Kind:  "local",
		})
		return
	}

	// This is the paid-model boundary. Deterministic local answers above stay free,
	// but any large-model request must belong to an active billed live runtime.
	runtimeSession, runtimeErr := s.store.GetLiveRuntimeByRoom(r.Context(), tenantID, roomID)
	if runtimeErr != nil {
		if errors.Is(runtimeErr, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, "请先启动直播搭子后再使用智能回答")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取智能体运行状态失败")
		return
	}
	if runtimeSession.Status != "running" {
		writeError(w, http.StatusConflict, "请先启动直播搭子后再使用智能回答")
		return
	}

	assistantName := s.configuredAgentName(r.Context(), actor.IsInternalStaff())
	baseSystemPrompt := s.store.RenderAgentPrompt(r.Context(), "agent.chat.system", "直接、真实、简洁地回答。", map[string]string{
		"assistant_name":       assistantName,
		"display_name":         strings.TrimSpace(settings.DisplayName),
		"role_name":            strings.TrimSpace(settings.RoleName),
		"self_introduction":    strings.TrimSpace(settings.SelfIntroduction),
		"mission":              strings.TrimSpace(settings.Mission),
		"current_time":         time.Now().In(liveAgentLocation()).Format("2006年1月2日 15:04:05 MST"),
		"question":             strings.TrimSpace(input.Message),
		"conversation_history": fmt.Sprintf("%v", input.History),
	})
	if !actor.IsInternalStaff() {
		guard := s.store.RenderAgentPrompt(r.Context(), "terminal.output.guard", "只使用普通用户可理解的自然业务语言。", map[string]string{
			"question": strings.TrimSpace(input.Message),
		})
		baseSystemPrompt = strings.TrimSpace(baseSystemPrompt + "\n\n" + guard)
	}
	reply, providerName, modelName, latencyMS, err := callLiveAgent(
		r.Context(),
		settings,
		assistantName,
		baseSystemPrompt,
		effectivePolicyPrompt,
		input,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "场控 Agent 暂时无法回答，请稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, liveAgentChatOutput{
		Reply:     reply,
		Kind:      "model",
		Provider:  providerName,
		Model:     modelName,
		LatencyMS: latencyMS,
	})
}

func liveAgentLocation() *time.Location {
	name := strings.TrimSpace(os.Getenv("LIVE_AGENT_TIMEZONE"))
	if name == "" {
		name = "Asia/Shanghai"
	}
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return location
}

func localLiveAgentAnswer(message string) (string, bool) {
	normalized := strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "",
		"，", "", "。", "", "？", "", "?", "", "！", "", "!", "",
	).Replace(message)
	now := time.Now().In(liveAgentLocation())
	for _, keyword := range []string{
		"现在几点", "几点了", "当前几点", "当前时间", "现在时间",
	} {
		if strings.Contains(normalized, keyword) {
			return fmt.Sprintf("现在是 %s。", now.Format("15:04")), true
		}
	}
	for _, keyword := range []string{
		"今天几号", "今天日期", "今天是几号", "今天星期几", "今天周几",
	} {
		if strings.Contains(normalized, keyword) {
			weekdays := []string{"日", "一", "二", "三", "四", "五", "六"}
			return fmt.Sprintf(
				"今天是 %s，星期%s。",
				now.Format("2006年1月2日"),
				weekdays[int(now.Weekday())],
			), true
		}
	}
	return "", false
}

func callLiveAgent(
	ctx context.Context,
	settings model.LiveAgentSettings,
	assistantName string,
	baseSystemPrompt string,
	effectivePolicyPrompt string,
	input liveAgentChatInput,
) (string, string, string, int64, error) {
	now := time.Now().In(liveAgentLocation())
	systemPrompt := strings.TrimSpace(baseSystemPrompt)
	replacer := strings.NewReplacer(
		"{{assistant_name}}", strings.TrimSpace(assistantName),
		"{{display_name}}", strings.TrimSpace(settings.DisplayName),
		"{{role_name}}", strings.TrimSpace(settings.RoleName),
		"{{self_introduction}}", strings.TrimSpace(settings.SelfIntroduction),
		"{{mission}}", strings.TrimSpace(settings.Mission),
		"{{current_time}}", now.Format("2006年1月2日 15:04:05 MST"),
	)
	systemPrompt = replacer.Replace(systemPrompt)
	if strings.TrimSpace(effectivePolicyPrompt) != "" {
		systemPrompt += "\n\n【当前直播间已生效业务策略】\n" + strings.TrimSpace(effectivePolicyPrompt)
	}

	messages := []agentgateway.Message{
		{Role: "system", Content: systemPrompt},
	}
	for _, item := range input.History {
		role := strings.TrimSpace(item.Role)
		if role == "agent" {
			role = "assistant"
		}
		if role != "assistant" && role != "user" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > 1200 {
			text = string([]rune(text)[:1200])
		}
		messages = append(messages, agentgateway.Message{Role: role, Content: text})
	}

	userContent := input.Message
	if input.AnchorTranscript != "" &&
		input.AnchorTranscript != "等待主播实时语音转写…" {
		userContent = fmt.Sprintf(
			"主播当前实时转写：%s\n\n后台操作者：%s",
			input.AnchorTranscript,
			input.Message,
		)
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: userContent})

	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages:       messages,
		MaxTokens:      500,
		EnableThinking: false,
		Timeout:        20 * time.Second,
	})
	if err != nil {
		return "", "", "", 0, err
	}
	return result.Text, result.Provider, result.Model, result.LatencyMS, nil
}
