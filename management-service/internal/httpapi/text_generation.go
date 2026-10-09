package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/anchorcycle"
	"livecompanion/management/internal/anchorstylescore"
	appdb "livecompanion/management/internal/db"
	"livecompanion/management/internal/interactionpolicy"
	"strings"
	"time"
)

func (s *Server) installCycleTextModel(ctx context.Context, state *anchorcycle.State) error {
	// New runs take the operator's current role; saved business/voice assets stay frozen.
	settings, err := s.store.AnchorGenerationSettings(ctx)
	if err != nil {
		return err
	}
	if settings.Cycle.TextProfileID != "" {
		state.Config.TextProfileID = settings.Cycle.TextProfileID
	}
	if state.Config.TextProfileID == "" {
		return nil
	} // Explicit legacy Agent configuration.
	profile, err := s.store.ResolveSpeechModel(ctx, state.Config.TextProfileID)
	if err != nil || profile == nil || !profile.Enabled || profile.APIKey == "" {
		return fmt.Errorf("主线文字模型不可用")
	}
	_, hash, _, err := agentgateway.TextPromptContract(profile.Model)
	if err != nil {
		return err
	}
	snapshot := *profile
	snapshot.APIKey = ""
	state.TextModel = &snapshot
	state.TextPromptSHA = hash
	state.SessionID = "text-" + state.RunID // Run identifier only; never an upstream session.
	return nil
}

// Compile expression data only. Legacy sample products, price examples and
// filled hook skeletons remain in their original assets, never in today's facts.
func cycleExpressionView(state *anchorcycle.State) (any, error) {
	b, err := anchorstylescore.Parse(state.BaselineJSON)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"语气词": b.Identity.Particles, "称呼": b.Identity.Audience, "自指": b.Identity.Self,
		"节奏":     map[string]any{"平均分句长度": b.Identity.Rhythm.Length, "长度区间": b.Identity.Rhythm.LengthRange, "问句频率": b.Identity.Rhythm.QuestionFrequency},
		"数字表达习惯": map[string]any{"价格表述方式": b.Identity.Numbers.Style, "自然帮观众算账": b.Identity.Numbers.LikesArithmetic},
		"量化基准":   b.Quant, "表达偏好": b.Forbidden,
	}, nil
}
func textControlLeak(text string) bool {
	for _, marker := range []string{"{{", "}}", "本次任务：", "表达展开建议：", "开头模式：", "后续表达加强建议：", "\"本次任务\":", "\"reply\":", "\"needs_review\":", "## 本场有效事实", "## 主播表达标尺"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}
func tailText(value string, limit int) string {
	r := []rune(value)
	if len(r) > limit {
		r = r[len(r)-limit:]
	}
	return string(r)
}
func renderCycleTextMessages(state *anchorcycle.State) ([]agentgateway.Message, error) {
	foundation, err := interactionpolicy.Read("mainline-text-task.md")
	if err != nil {
		return nil, err
	}
	view, err := cycleExpressionView(state)
	if err != nil {
		return nil, err
	}
	task := state.Task()
	// Stable prefix: complete current product facts and a reusable expression view.
	fixed, _ := json.Marshal(map[string]any{"本场有效事实": state.ProductFacts(task), "主播表达标尺": view, "有效P0": state.P0PolicyMD})
	recent := []string{}
	for _, part := range state.Accepted[max(0, len(state.Accepted)-8):] {
		recent = append(recent, cycleVoiceText(part.Selected))
	}
	feedback := state.PendingFeedback
	if len(state.Attempts) > 0 {
		feedback += "\n" + state.Attempts[len(state.Attempts)-1].RetryFeedback
	}
	maxChars := state.Config.MaxChars
	if task.ProductMaxChars > 0 && task.ProductMaxChars < maxChars {
		maxChars = task.ProductMaxChars
	}
	// Vary explanation methods within the selected purpose/angle, not the task itself.
	methods := []string{"解释选择依据", "从具体顾虑说起", "说明日常用途", "用自然对话展开", "解释价值而非只报价格", "讲清条件和边界"}
	opener := "address_or_content"
	if state.TotalAccepted()%3 != 0 {
		opener = "content_first"
	}
	packet := map[string]any{"开头模式": opener, "本次任务": task, "环节": state.Phase, "开场版本": state.OpeningVersion(), "本轮编号": state.CycleNumber, "中间段计划": state.BodyLimit(), "已完成中间段": state.BodyCount, "字数上限": maxChars, "表达展开建议": methods[(state.TotalAccepted()+state.CycleNumber)%len(methods)], "近期已采用内容仅供避重": tailText(strings.Join(recent, "\n"), 2400), "后续表达加强建议": tailText(feedback, 1200)}
	if hook := state.PendingOpeningHook; state.Phase == "opening" && hook != nil {
		packet["开头沟通意图"] = map[string]any{"类型": hook.Kind, "需要说明的事实条件": hook.HardValues, "关注对象与顾虑": hook.SoftValues}
	}
	if state.PriceHook != nil {
		packet["上一段待解释的问题"] = state.PriceHook.Promise
	}
	if strategy, err := state.ContentStrategy.Render("主线", state.ProductFacts(task)); err != nil {
		return nil, err
	} else if strategy != "" {
		packet["内容方向与采用条件"] = strategy
	}
	if state.P0PendingRepair != nil {
		repair, err := renderP0Repair(state)
		if err != nil {
			return nil, err
		}
		packet["本次明确违规修正"] = repair
	}
	dynamic, _ := json.Marshal(packet)
	return []agentgateway.Message{{Role: "system", Content: string(foundation) + "\n\n" + string(fixed)}, {Role: "user", Content: string(dynamic)}}, nil
}
func (s *Server) completeCycleText(ctx context.Context, state *anchorcycle.State) (agentgateway.Response, []agentgateway.Message, error) {
	messages, err := renderCycleTextMessages(state)
	if err != nil {
		return agentgateway.Response{}, nil, err
	}
	current, err := s.store.ResolveSpeechModel(ctx, state.Config.TextProfileID)
	if err != nil || current == nil || !current.Enabled || current.APIKey == "" || state.TextModel == nil {
		return agentgateway.Response{}, messages, fmt.Errorf("本场文字模型凭据不可用")
	}
	profile := *state.TextModel
	profile.APIKey = current.APIKey
	req := agentgateway.Request{Stage: "mainline_text", Model: profile.Model, Messages: messages, ResponseFormat: agentgateway.ResponseText, MaxTokens: 1800, EnableThinking: false, Timeout: time.Duration(profile.TimeoutMS) * time.Millisecond}
	if profile.SystemPrompt != "" {
		req.Messages = append([]agentgateway.Message{{Role: "system", Content: profile.SystemPrompt}}, req.Messages...)
	}
	req, err = agentgateway.PrepareTextRequest(profile, req)
	if err != nil {
		return agentgateway.Response{}, messages, err
	}
	state.TextPromptSHA = req.TextPromptSHA
	ctx = withExchangeScope(ctx, appdb.ModelExchangeScope{TenantID: state.TenantID, PlanID: state.PlanID, RoomID: state.RoomID, RunID: state.RunID})
	id, err := s.beginModelExchange(ctx, "mainline_text", profile.ID, req)
	if err != nil {
		return agentgateway.Response{}, req.Messages, err
	}
	started := time.Now()
	response, err := agentgateway.CompleteSpeechEndpoint(ctx, profile, req)
	response.Provider, response.Model = "anchor:"+profile.ID, profile.Model
	err = s.finishModelExchange(ctx, id, response, response.Text, started, response.InputTokens, response.OutputTokens, err)
	return response, req.Messages, err
}
