package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/agentmemory"
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
	ImageURLs        []string                   `json:"image_urls,omitempty"`
}

const (
	maxAgentChatImages          = 4
	maxAgentChatImageDataURLLen = 2200000
	maxAgentChatImageTotalLen   = 5600000
)

func sanitizeAgentChatImageURLs(values []string) ([]string, error) {
	if len(values) > maxAgentChatImages {
		return nil, fmt.Errorf("单次最多引用 %d 张图片", maxAgentChatImages)
	}
	result := make([]string, 0, len(values))
	total := 0
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		comma := strings.IndexByte(value, ',')
		if comma <= 0 || comma >= len(value)-1 {
			return nil, fmt.Errorf("图片数据格式错误")
		}
		header := strings.ToLower(value[:comma])
		switch header {
		case "data:image/png;base64", "data:image/jpeg;base64", "data:image/webp;base64":
		default:
			return nil, fmt.Errorf("仅支持 PNG、JPEG、WEBP 图片")
		}
		if len(value) > maxAgentChatImageDataURLLen {
			return nil, fmt.Errorf("图片过大，请重新粘贴较小图片")
		}
		if _, err := base64.StdEncoding.DecodeString(value[comma+1:]); err != nil {
			return nil, fmt.Errorf("图片数据损坏")
		}
		total += len(value)
		if total > maxAgentChatImageTotalLen {
			return nil, fmt.Errorf("本次引用图片总大小过大")
		}
		result = append(result, value)
	}
	return result, nil
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
	if err := readAgentJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "场控 Agent 请求格式错误")
		return
	}
	var imageErr error
	input.ImageURLs, imageErr = sanitizeAgentChatImageURLs(input.ImageURLs)
	if imageErr != nil {
		writeError(w, http.StatusBadRequest, imageErr.Error())
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
	memories, memoryErr := s.store.ListActiveAgentMemories(r.Context(), tenantID, roomID)
	if memoryErr != nil {
		writeError(w, http.StatusInternalServerError, "读取当前直播间智能体记忆失败")
		return
	}
	if memoryPrompt := agentmemory.Prompt(memories); memoryPrompt != "" {
		effectivePolicyPrompt = strings.TrimSpace(effectivePolicyPrompt + "\n\n" + memoryPrompt)
	}

	if liveAgentChatMutationIntent(input.Message) {
		writeJSON(w, http.StatusOK, liveAgentChatOutput{
			Reply: "这条聊天消息本身没有执行保存或发布。请先完成智能体学习修正，再使用“采用”或“保存发布”；只有系统真实写入并返回版本号后才算生效。",
			Kind:  "local",
		})
		return
	}

	if reply, matched := localLiveAgentAnswer(input.Message); matched {
		writeJSON(w, http.StatusOK, liveAgentChatOutput{
			Reply: reply,
			Kind:  "local",
		})
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
	baseSystemPrompt = strings.TrimSpace(baseSystemPrompt + `

【长期搭档式交流】
- 你不仅能处理直播业务，也可以正常聊天。用户聊日常、情绪、吐槽或轻松话题时，自然接话，不要硬把话题拉回直播业务。
- 语气像长期合作的工作搭档：亲近、自然、简洁，可以适度幽默，但不要装熟、过度热情或制造情感依赖。
- 不要假装自己有真实身体或现实生活经历。
- 只有用户明确要求修改、记录、采用某项业务规则时，才把它理解为业务修改；普通聊天不能自动变成规则或事实。

【执行真实性】
- 当前聊天接口只负责理解、整理和回答，本接口本身不会写数据库、发布配置或让活动生效。
- 在没有真实写入接口返回成功结果时，禁止说“已记录”“已更新”“已保存”“已发布”“已生效”“已经添加”等完成态文案。
- 用户提出新增、修改、保存、采用等要求时，可以整理成候选、指出缺失字段或提示用户确认，但必须明确说明“尚未写入/需要确认”。
- 只有外层业务动作真实执行成功并返回版本或写入结果后，才由系统界面告知用户已经完成。
`)
	roomRef := roomID
	invocationID := s.beginAISingleUse(r.Context(), actor, &roomRef, "live_agent", map[string]any{"mode": "chat"})
	reply, providerName, modelName, latencyMS, err := callLiveAgent(
		r.Context(),
		settings,
		assistantName,
		baseSystemPrompt,
		effectivePolicyPrompt,
		input,
	)
	if err != nil {
		s.finishAISingleUse(r.Context(), invocationID, "failed", providerName, modelName, latencyMS, map[string]any{"error": err.Error()})
		writeError(w, http.StatusBadGateway, "场控 Agent 暂时无法回答，请稍后再试")
		return
	}
	s.finishAISingleUse(r.Context(), invocationID, "succeeded", providerName, modelName, latencyMS, nil)
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

func liveAgentChatMutationIntent(message string) bool {
	normalized := strings.NewReplacer(
		" ", "", "\t", "", "\r", "", "\n", "",
		"，", "", ",", "", "。", "", ".", "", "？", "", "?", "", "！", "", "!", "",
	).Replace(strings.TrimSpace(message))
	switch normalized {
	case "采用", "保存", "发布", "保存发布", "保存并发布", "保存采用", "保存并采用",
		"就按这个", "按这个来", "用这个", "就这样", "确定采用":
		return true
	default:
		return false
	}
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
	if len(input.ImageURLs) > 0 {
		systemPrompt += "\n\n【图片理解要求】用户在本轮附带了图片。结合图片中直接可见的文字、布局、控件、商品和对象理解用户说的‘这里/这个/图里’具体指什么；看不清就明确说明，不得猜测。图片只是本轮上下文，不代表用户已经确认任何写入动作，涉及修改或采用仍遵守原有确认流程。"
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
	messages = append(messages, agentgateway.Message{Role: "user", Content: userContent, ImageURLs: input.ImageURLs})

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
