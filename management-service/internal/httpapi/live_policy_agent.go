package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type livePolicyAgentInput struct {
	Layer        string                     `json:"layer,omitempty"`
	IndustryCode string                     `json:"industry_code,omitempty"`
	Message      string                     `json:"message"`
	History      []liveAgentChatHistoryItem `json:"history,omitempty"`
	Scene        string                     `json:"scene,omitempty"`
	ImageURLs    []string                   `json:"image_urls,omitempty"`
}

type livePolicyAgentModelOutput struct {
	Action           string                     `json:"action"`
	AssistantMessage string                     `json:"assistant_message"`
	SourceText       string                     `json:"source_text"`
	Rules            []model.LivePolicyRule     `json:"rules"`
	Overrides        []model.LivePolicyOverride `json:"overrides"`
	Conflicts        []model.LivePolicyConflict `json:"conflicts"`
	Note             string                     `json:"note,omitempty"`
}

type livePolicyAgentOutput struct {
	Reply     string                     `json:"reply"`
	Target    string                     `json:"target,omitempty"`
	Action    string                     `json:"action"`
	Draft     *model.LivePolicyVersion   `json:"draft,omitempty"`
	Conflicts []model.LivePolicyConflict `json:"conflicts,omitempty"`
	Model     string                     `json:"model,omitempty"`
	LatencyMS int64                      `json:"latency_ms,omitempty"`
}

type livePolicyConversationModelOutput struct {
	Reply  string `json:"reply"`
	Target string `json:"target,omitempty"`
}

func (s *Server) livePolicyAdminAgentChat(w http.ResponseWriter, r *http.Request) {
	var input livePolicyAgentInput
	if err := readAgentJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "策略 Agent 请求格式错误")
		return
	}
	var imageErr error
	input.ImageURLs, imageErr = sanitizeAgentChatImageURLs(input.ImageURLs)
	if imageErr != nil {
		writeError(w, http.StatusBadRequest, imageErr.Error())
		return
	}
	input.Layer = strings.ToUpper(strings.TrimSpace(input.Layer))
	if input.Layer == "" {
		input.Layer = model.LivePolicyLayerL1
	}
	actor, ok := s.requireLivePolicyLayerManage(w, r, input.Layer)
	if !ok {
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
	if input.Message == "" {
		writeError(w, http.StatusBadRequest, "请输入要调教的内容")
		return
	}
	if utf8.RuneCountInString(input.Message) > 6000 {
		writeError(w, http.StatusBadRequest, "单次调教内容不能超过 6000 字")
		return
	}
	if input.Layer == model.LivePolicyLayerL2 && input.IndustryCode == "" {
		input.IndustryCode = "general"
	}
	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), input.Layer, input.IndustryCode, 0, 0, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusBadRequest, "初始化策略作用域失败")
		return
	}

	current, _ := s.store.GetActiveLivePolicyVersion(
		r.Context(), input.Layer, input.IndustryCode, 0, 0,
	)
	if scope, scopeErr := s.store.GetLivePolicyScope(
		r.Context(), input.Layer, input.IndustryCode, 0, 0,
	); scopeErr == nil {
		if versions, listErr := s.store.ListLivePolicyVersions(r.Context(), scope.ID); listErr == nil {
			for index := range versions {
				if versions[index].LifecycleStatus == "draft" {
					candidate := versions[index]
					current = &candidate
					break
				}
			}
		}
	}
	if current != nil {
		stabilized := *current
		stabilized.Rules = policy.EnsureStableRuleKeys(input.Layer, current.Rules, nil)
		current = &stabilized
	}
	l1, _ := s.store.GetActiveLivePolicyVersion(
		r.Context(), model.LivePolicyLayerL1, "", 0, 0,
	)
	assistantName := s.configuredAgentName(r.Context(), true)
	adminInstruction := s.store.AgentPromptValue(r.Context(), "policy.agent.admin", "只在当前权限范围内协助管理直播策略，并严格返回 JSON。")
	prompt, err := buildAdminPolicyAgentPrompt(adminInstruction, input.Layer, input.IndustryCode, current, l1, assistantName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "准备策略上下文失败")
		return
	}
	modelOutput, modelName, latencyMS, err := callPolicyAgentModel(
		r.Context(), prompt, input.Message, input.History, input.ImageURLs,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "策略 Agent 暂时无法回答，请稍后再试")
		return
	}
	if normalizePolicyAgentAction(modelOutput.Action) == "DRAFT" &&
		!adminPolicyModelRulesComplete(modelOutput.Rules) {
		repairInstruction := s.store.AgentPromptValue(r.Context(), "policy.agent.repair", "上一轮草稿结构不完整，请重新输出完整 JSON。")
		repairPrompt := strings.TrimSpace(prompt + "\n\n【结构修复要求】\n" + repairInstruction)
		repaired, repairedModel, repairedLatency, repairErr := callPolicyAgentModel(
			r.Context(), repairPrompt, input.Message, input.History, input.ImageURLs,
		)
		if repairErr != nil {
			writeError(w, http.StatusBadGateway, "策略 Agent 草稿结构不完整，自动修复失败，请重试")
			return
		}
		modelOutput = repaired
		modelName = repairedModel
		latencyMS += repairedLatency
		if normalizePolicyAgentAction(modelOutput.Action) != "DRAFT" ||
			!adminPolicyModelRulesComplete(modelOutput.Rules) {
			writeError(w, http.StatusBadGateway, "策略 Agent 返回的规则正文不完整，本次未保存草稿，请重试")
			return
		}
	}

	output := livePolicyAgentOutput{
		Reply:     strings.TrimSpace(modelOutput.AssistantMessage),
		Action:    normalizePolicyAgentAction(modelOutput.Action),
		Conflicts: modelOutput.Conflicts,
		Model:     modelName,
		LatencyMS: latencyMS,
	}
	if output.Reply == "" {
		output.Reply = "我已经理解你的要求。"
	}
	if output.Action != "DRAFT" {
		writeJSON(w, http.StatusOK, output)
		return
	}

	baseRules := []model.LivePolicyRule{}
	if current != nil {
		baseRules = current.Rules
	}
	draftInput := model.CreateLivePolicyDraftInput{
		Layer:        input.Layer,
		IndustryCode: input.IndustryCode,
		SourceText:   strings.TrimSpace(modelOutput.SourceText),
		Rules: policy.PrioritizeNewRules(
			policy.EnsureStableRuleKeys(input.Layer, modelOutput.Rules, baseRules),
			baseRules,
		),
		Conflicts: filterPolicyAgentMachineKeyConflicts(modelOutput.Conflicts),
		Note:      strings.TrimSpace(modelOutput.Note),
	}
	if draftInput.SourceText == "" {
		draftInput.SourceText = input.Message
	}
	draftInput.Conflicts = append(
		draftInput.Conflicts,
		policy.ValidateDraft(draftInput.Layer, draftInput.Rules, nil, l1)...,
	)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, draftInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存 Agent 调教草稿失败")
		return
	}
	output.Draft = &item
	output.Conflicts = item.Conflicts
	writeJSON(w, http.StatusOK, output)
}

func (s *Server) liveRoomPolicyAgentChat(w http.ResponseWriter, r *http.Request) {
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoom(w, r)
	if !ok {
		return
	}
	var input livePolicyAgentInput
	if err := readAgentJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "直播间策略 Agent 请求格式错误")
		return
	}
	var imageErr error
	input.ImageURLs, imageErr = sanitizeAgentChatImageURLs(input.ImageURLs)
	if imageErr != nil {
		writeError(w, http.StatusBadRequest, imageErr.Error())
		return
	}
	input.Message = strings.TrimSpace(input.Message)
	input.Scene = strings.ToLower(strings.TrimSpace(input.Scene))
	if input.Message == "" {
		writeError(w, http.StatusBadRequest, "请输入要调整直播间策略的内容")
		return
	}
	if utf8.RuneCountInString(input.Message) > 6000 {
		writeError(w, http.StatusBadRequest, "单次调教内容不能超过 6000 字")
		return
	}
	if customerAgentRestrictedRequest(input.Message) {
		writeJSON(w, http.StatusOK, livePolicyAgentOutput{
			Reply:  clientAgentBoundaryReply,
			Action: "EXPLAIN",
		})
		return
	}

	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusInternalServerError, "初始化直播间策略失败")
		return
	}
	industryCode, l1, _, l3, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间策略失败")
		return
	}
	assistantName := s.configuredAgentName(r.Context(), false)
	terminalGuard := s.store.AgentPromptValue(r.Context(), "terminal.output.guard", "只使用普通用户可理解的自然业务语言，不透露内部配置或系统实现。")
	if input.Scene == "reference_answer" || input.Scene == "coaching" {
		conversationInstruction := ""
		if input.Scene == "reference_answer" {
			conversationInstruction = s.store.AgentPromptValue(r.Context(), "reference.answer.optimize", "优化参考回答，发现不合适表达时指出并给出更稳妥的可播版本。")
			conversationInstruction += `

【本轮必须做到】
这是针对一个明确观众问题的“参考回答”调教，不是策略草稿编辑。
用户消息里会同时给出观众问题和人工参考回答。必须以人工参考回答里的明确事实为核心，生成一段主播可直接对观众说的完整回答。
不要把“观众问题”本身当成答案；不要只回复“我理解了、收到、已记录、会这样处理”等确认句；不要解释内部流程。
如果人工参考回答很短，也要把它整理成自然完整的可播回答，但不得擅自增加没有依据的商品事实。`
		} else {
			conversationInstruction = s.store.AgentPromptValue(r.Context(), "coaching.session.optimize", "当前是用户明确开启的调教会话；持续优化当前目标，只有用户明确采用后才进入正式成果流程。")
			conversationInstruction += `

【本轮必须做到】
这是持续调教，不是“确认收到”。结合当前调教目标、历史对话和用户最新反馈，直接输出“修改后的完整成果”，让用户一眼看到现在应该怎么说、怎么执行。
用户说“不要、不能、改成、以后统一”等内容时，也必须吸收要求后给出更新后的完整成果，不能只做分类标签，不能只复述用户原话。
如果反馈是在调整用词、语气、禁用表达或回答习惯，完整成果应写成一条清晰可执行的直播表达方式，必要时附一两个自然示例，让人可以直接判断这版是否满意。
同时给出一个简短明确的 target，表示这一轮实际在调教什么，例如“菜籽油用词方式”“物流回答方式”。target 不能沿用无关旧聊天。
不要输出“我已经理解、收到、已记录”等空确认；不要声称已经发布或生效。`
		}
		prompt, promptErr := buildRoomPolicyAgentPrompt(conversationInstruction, industryCode, l3, assistantName)
		if promptErr != nil {
			writeError(w, http.StatusInternalServerError, "准备直播间调教上下文失败")
			return
		}
		prompt = strings.TrimSpace(prompt + "\n\n【终端输出要求】\n" + terminalGuard + `

【输出格式】
只返回严格 JSON：{"reply":"这里放完整成果","target":"本轮调教目标"}。
reply 必须是本轮真正的完整成果正文，不得为空，不得是确认句。reference_answer 场景的 target 可以留空；coaching 场景必须给出简短准确的 target。`)
		reply, target, modelName, latencyMS, modelErr := callPolicyConversationModel(r.Context(), prompt, input.Message, input.History, input.ImageURLs)
		if modelErr != nil {
			writeError(w, http.StatusBadGateway, "直播间调教暂时无法生成有效成果，请稍后重试")
			return
		}
		writeJSON(w, http.StatusOK, livePolicyAgentOutput{
			Reply:     reply,
			Target:    target,
			Action:    "EXPLAIN",
			Model:     modelName,
			LatencyMS: latencyMS,
		})
		return
	}

	roomInstruction := s.store.AgentPromptValue(r.Context(), "policy.agent.room", "只处理当前直播间自己的策略，并严格返回 JSON。")
	prompt, err := buildRoomPolicyAgentPrompt(roomInstruction, industryCode, l3, assistantName)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "准备直播间策略上下文失败")
		return
	}
	prompt = strings.TrimSpace(prompt + "\n\n【终端输出要求】\n" + terminalGuard)
	modelOutput, modelName, latencyMS, err := callPolicyAgentModel(
		r.Context(), prompt, input.Message, input.History, input.ImageURLs,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "直播间策略 Agent 暂时无法回答，请稍后再试")
		return
	}

	output := livePolicyAgentOutput{
		Reply:     strings.TrimSpace(modelOutput.AssistantMessage),
		Action:    normalizePolicyAgentAction(modelOutput.Action),
		Conflicts: customerSafePolicyConflicts(modelOutput.Conflicts),
		Model:     modelName,
		LatencyMS: latencyMS,
	}
	if output.Reply == "" {
		output.Reply = "我已经理解你对这个直播间的调整。"
	}
	if output.Action != "DRAFT" {
		writeJSON(w, http.StatusOK, output)
		return
	}

	baseOverrides := []model.LivePolicyOverride{}
	if l3 != nil {
		baseOverrides = l3.Overrides
	}
	draftInput := model.CreateLivePolicyDraftInput{
		Layer:      model.LivePolicyLayerL3,
		TenantID:   tenantID,
		RoomID:     roomID,
		SourceText: strings.TrimSpace(modelOutput.SourceText),
		Overrides:  policy.EnsureStableOverrideKeys(modelOutput.Overrides, baseOverrides),
		Conflicts:  filterPolicyAgentMachineKeyConflicts(modelOutput.Conflicts),
		Note:       strings.TrimSpace(modelOutput.Note),
	}
	if draftInput.SourceText == "" {
		draftInput.SourceText = input.Message
	}
	draftInput.Conflicts = append(
		draftInput.Conflicts,
		policy.ValidateDraft(model.LivePolicyLayerL3, nil, draftInput.Overrides, l1)...,
	)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, draftInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存直播间策略草稿失败")
		return
	}
	safeItem := customerSafeLivePolicyVersion(item)
	output.Draft = &safeItem
	output.Conflicts = customerSafePolicyConflicts(item.Conflicts)
	writeJSON(w, http.StatusOK, output)
}

func buildAdminPolicyAgentPrompt(
	instruction string,
	layer, industryCode string,
	current, l1 *model.LivePolicyVersion,
	assistantName string,
) (string, error) {
	currentRaw, err := json.Marshal(current)
	if err != nil {
		return "", err
	}
	l1Raw, err := json.Marshal(l1)
	if err != nil {
		return "", err
	}
	layerName := "规则层"
	if layer == model.LivePolicyLayerL2 {
		layerName = "行业层"
	}
	return strings.TrimSpace(instruction + fmt.Sprintf(`

【当前工作上下文】
智能体名称：%s
当前编辑：%s
行业代码：%s
当前规则层数据：%s
当前编辑层数据：%s
`, strings.TrimSpace(assistantName), layerName, industryCode, string(l1Raw), string(currentRaw))), nil
}

func buildRoomPolicyAgentPrompt(
	instruction string,
	industryCode string,
	l3 *model.LivePolicyVersion,
	assistantName string,
) (string, error) {
	l3Raw, err := json.Marshal(customerSafeLivePolicyVersionPtr(l3))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(instruction + fmt.Sprintf(`

【当前直播间上下文】
智能体名称：%s
行业代码：%s
当前直播间策略数据：%s
`, strings.TrimSpace(assistantName), industryCode, string(l3Raw))), nil
}

func adminPolicyModelRulesComplete(rules []model.LivePolicyRule) bool {
	if len(rules) == 0 {
		return false
	}
	for _, rule := range rules {
		if strings.TrimSpace(rule.Title) == "" || strings.TrimSpace(rule.Text) == "" {
			return false
		}
		switch strings.ToLower(strings.TrimSpace(rule.ExecutionMode)) {
		case model.LivePolicyModeIntent:
		case model.LivePolicyModeVerbatim:
			if strings.TrimSpace(rule.FixedText) == "" {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func filterPolicyAgentMachineKeyConflicts(
	conflicts []model.LivePolicyConflict,
) []model.LivePolicyConflict {
	result := make([]model.LivePolicyConflict, 0, len(conflicts))
	for _, conflict := range conflicts {
		switch strings.TrimSpace(conflict.Code) {
		case "missing_rule_key", "duplicate_rule_key", "missing_override_key", "duplicate_override_key":
			continue
		default:
			result = append(result, conflict)
		}
	}
	return result
}

func normalizePolicyAgentAction(action string) string {
	if strings.EqualFold(strings.TrimSpace(action), "DRAFT") {
		return "DRAFT"
	}
	return "EXPLAIN"
}

func isEmptyCoachingAcknowledgement(value string) bool {
	compact := strings.NewReplacer(" ", "", "\n", "", "\r", "", "。", "", "！", "", "!", "").Replace(strings.TrimSpace(value))
	if compact == "" {
		return true
	}
	for _, prefix := range []string{"我已经理解", "我理解了", "已经理解", "收到", "已收到", "已记录", "我已经记录", "好的我会", "明白我会"} {
		if strings.HasPrefix(compact, prefix) && utf8.RuneCountInString(compact) <= 40 {
			return true
		}
	}
	return false
}

func callPolicyConversationModel(
	ctx context.Context,
	systemPrompt, message string,
	history []liveAgentChatHistoryItem,
	imageURLs []string,
) (string, string, string, int64, error) {
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	if len(imageURLs) > 0 {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n【图片理解要求】用户附带的图片是本轮调教上下文。结合图片中直接可见的文字、布局和对象理解用户指向；看不清不要猜，图片本身不代表已经采用或发布。")
	}
	messages := []agentgateway.Message{{Role: "system", Content: systemPrompt}}
	for _, item := range history {
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
		if utf8.RuneCountInString(text) > 1600 {
			text = string([]rune(text)[:1600])
		}
		messages = append(messages, agentgateway.Message{Role: role, Content: text})
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: message, ImageURLs: imageURLs})

	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages:       messages,
		MaxTokens:      1000,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return "", "", "", 0, err
	}
	var output livePolicyConversationModelOutput
	if err := json.Unmarshal([]byte(stripPolicyJSONFence(result.Text)), &output); err != nil {
		return "", "", "", result.LatencyMS, fmt.Errorf("decode coaching output: %w", err)
	}
	reply := strings.TrimSpace(output.Reply)
	target := strings.TrimSpace(output.Target)
	if reply == "" || isEmptyCoachingAcknowledgement(reply) {
		return "", "", "", result.LatencyMS, fmt.Errorf("empty or acknowledgement-only coaching result")
	}
	return reply, target, result.Model, result.LatencyMS, nil
}

func callPolicyAgentModel(
	ctx context.Context,
	systemPrompt, message string,
	history []liveAgentChatHistoryItem,
	imageURLs []string,
) (livePolicyAgentModelOutput, string, int64, error) {
	if len(history) > 12 {
		history = history[len(history)-12:]
	}
	if len(imageURLs) > 0 {
		systemPrompt = strings.TrimSpace(systemPrompt + "\n\n【图片理解要求】用户附带的图片是本轮策略上下文。先依据图片中直接可见的文字、页面结构、控件和对象理解‘这个/这里’的具体指向；不清楚就说明，不得猜。图片不代表用户授权发布或写入，仍按原有草稿、冲突检查和发布流程处理。")
	}
	messages := []agentgateway.Message{{Role: "system", Content: systemPrompt}}
	for _, item := range history {
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
		if utf8.RuneCountInString(text) > 2000 {
			text = string([]rune(text)[:2000])
		}
		messages = append(messages, agentgateway.Message{Role: role, Content: text})
	}
	messages = append(messages, agentgateway.Message{Role: "user", Content: message, ImageURLs: imageURLs})

	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Messages:       messages,
		MaxTokens:      2200,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return livePolicyAgentModelOutput{}, "", 0, err
	}

	raw := stripPolicyJSONFence(result.Text)
	var output livePolicyAgentModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return livePolicyAgentModelOutput{}, "", result.LatencyMS, fmt.Errorf("decode policy agent output: %w", err)
	}
	output.Action = normalizePolicyAgentAction(output.Action)
	if output.Rules == nil {
		output.Rules = []model.LivePolicyRule{}
	}
	if output.Overrides == nil {
		output.Overrides = []model.LivePolicyOverride{}
	}
	if output.Conflicts == nil {
		output.Conflicts = []model.LivePolicyConflict{}
	}
	return output, result.Model, result.LatencyMS, nil
}

func stripPolicyJSONFence(raw string) string {
	value := strings.TrimSpace(raw)
	fence := "```"
	if strings.HasPrefix(value, fence) {
		value = strings.TrimPrefix(value, fence+"json")
		value = strings.TrimPrefix(value, fence+"JSON")
		value = strings.TrimPrefix(value, fence)
		value = strings.TrimSpace(value)
		if strings.HasSuffix(value, fence) {
			value = strings.TrimSpace(strings.TrimSuffix(value, fence))
		}
	}
	return value
}
