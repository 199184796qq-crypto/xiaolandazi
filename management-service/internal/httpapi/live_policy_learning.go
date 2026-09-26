package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"livecompanion/management/internal/agentgateway"
	"livecompanion/management/internal/model"
	"livecompanion/management/internal/policy"
)

type livePolicyLearningModelOutput struct {
	AbsorbRecommended bool   `json:"absorb_recommended"`
	TargetLayer       string `json:"target_layer"`
	Reason            string `json:"reason"`
	Confidence        int    `json:"confidence"`
	RuleTitle         string `json:"rule_title"`
	RuleText          string `json:"rule_text"`
	ExecutionMode     string `json:"execution_mode"`
}

type livePolicyLearningEvidenceProposal struct {
	AbsorbRecommended bool     `json:"absorb_recommended"`
	TargetLayer       string   `json:"target_layer"`
	Reason            string   `json:"reason"`
	Confidence        int      `json:"confidence"`
	RuleTitle         string   `json:"rule_title"`
	RuleText          string   `json:"rule_text"`
	ExecutionMode     string   `json:"execution_mode"`
	PromotionLevel    string   `json:"promotion_level"`
	RegressionCases   []string `json:"regression_cases"`
}

type livePolicyLearningEvidenceModelOutput struct {
	Summary   string                               `json:"summary"`
	Proposals []livePolicyLearningEvidenceProposal `json:"proposals"`
}

func normalizeLearningEvidenceType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "manual_feedback", "bad_answer", "corrected_answer", "screenshot_review", "post_live_review", "gold_sample":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "manual_feedback"
	}
}

func normalizeLearningPromotionLevel(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "stable":
		return "stable"
	case "guardrail":
		return "guardrail"
	default:
		return "candidate"
	}
}

func normalizeLearningRegressionCases(items []string) []string {
	return normalizeUniqueStrings(items, 8)
}

func normalizeLivePolicyLearningHistory(
	items []model.LivePolicyLearningHistoryItem,
) []model.LivePolicyLearningHistoryItem {
	if len(items) > 16 {
		items = items[len(items)-16:]
	}
	result := make([]model.LivePolicyLearningHistoryItem, 0, len(items))
	for _, item := range items {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		if role == "assistant" {
			role = "agent"
		}
		if role != "user" && role != "agent" {
			continue
		}
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		if utf8.RuneCountInString(text) > 2000 {
			text = string([]rune(text)[:2000])
		}
		result = append(result, model.LivePolicyLearningHistoryItem{
			Role: role,
			Text: text,
		})
	}
	return result
}

func buildLivePolicyLearningPrompt(instruction string, input model.CreateLivePolicyLearningCandidateInput) (string, error) {
	historyRaw, err := json.Marshal(input.History)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(instruction + fmt.Sprintf(`

【本次证据】
证据类型：%s
证据来源：%s
当前来源：%s
当前行业：%s
当前直播间ID：%d
原始问题：%s
当时真实回答：%s
最终满意回复/人工改写：%s
用户最后的调教/确认：%s
多轮调教历史：%s
`, input.EvidenceType, input.SourceRef, input.SourceLayer, input.IndustryCode, input.RoomID, input.Question, input.ObservedReply, input.FinalReply, input.Feedback, string(historyRaw))), nil
}

func normalizeLivePolicyLearningModelOutput(
	output livePolicyLearningModelOutput,
	input model.CreateLivePolicyLearningCandidateInput,
) livePolicyLearningModelOutput {
	output.TargetLayer = strings.ToUpper(strings.TrimSpace(output.TargetLayer))
	if output.TargetLayer != model.LivePolicyLayerL1 &&
		output.TargetLayer != model.LivePolicyLayerL2 &&
		output.TargetLayer != model.LivePolicyLayerL3 {
		output.TargetLayer = strings.ToUpper(strings.TrimSpace(input.SourceLayer))
		if output.TargetLayer != model.LivePolicyLayerL1 &&
			output.TargetLayer != model.LivePolicyLayerL2 &&
			output.TargetLayer != model.LivePolicyLayerL3 {
			if input.RoomID > 0 {
				output.TargetLayer = model.LivePolicyLayerL3
			} else {
				output.TargetLayer = model.LivePolicyLayerL1
			}
		}
	}
	output.Reason = strings.TrimSpace(output.Reason)
	output.RuleTitle = strings.TrimSpace(output.RuleTitle)
	output.RuleText = strings.TrimSpace(output.RuleText)
	output.ExecutionMode = strings.ToLower(strings.TrimSpace(output.ExecutionMode))
	if output.ExecutionMode != model.LivePolicyModeVerbatim {
		output.ExecutionMode = model.LivePolicyModeIntent
	}
	if output.Confidence < 0 {
		output.Confidence = 0
	}
	if output.Confidence > 100 {
		output.Confidence = 100
	}
	if output.RuleTitle == "" {
		output.RuleTitle = "调教沉淀规则"
	}
	if output.RuleText == "" {
		output.RuleText = strings.TrimSpace(input.Feedback)
		if output.RuleText == "" {
			output.RuleText = strings.TrimSpace(input.FinalReply)
		}
	}
	if output.Reason == "" {
		output.Reason = "根据调教内容的适用范围判断。"
	}
	return output
}

func callPolicyLearningModel(
	ctx context.Context,
	prompt string,
	provider string,
	modelName string,
) (livePolicyLearningModelOutput, string, string, int64, error) {
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Provider: provider,
		Model:    modelName,
		Messages: []agentgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: "请分析这次调教并返回 JSON。"},
		},
		MaxTokens:      1200,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        30 * time.Second,
	})
	if err != nil {
		return livePolicyLearningModelOutput{}, "", "", 0, err
	}
	raw := stripPolicyJSONFence(result.Text)
	var output livePolicyLearningModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return livePolicyLearningModelOutput{}, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode policy learning output: %w", err)
	}
	return output, result.Provider, result.Model, result.LatencyMS, nil
}

func (s *Server) livePolicyLearningCreateCandidate(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input model.CreateLivePolicyLearningCandidateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "调教学习内容格式错误")
		return
	}
	input.SourceLayer = strings.ToUpper(strings.TrimSpace(input.SourceLayer))
	input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
	input.EvidenceType = normalizeLearningEvidenceType(input.EvidenceType)
	input.SourceRef = strings.TrimSpace(input.SourceRef)
	input.ObservedReply = strings.TrimSpace(input.ObservedReply)
	input.LearningProvider = strings.TrimSpace(input.LearningProvider)
	input.LearningModel = strings.TrimSpace(input.LearningModel)
	input.Question = strings.TrimSpace(input.Question)
	input.FinalReply = strings.TrimSpace(input.FinalReply)
	input.Feedback = strings.TrimSpace(input.Feedback)
	input.History = normalizeLivePolicyLearningHistory(input.History)

	if input.SourceLayer != model.LivePolicyLayerL1 &&
		input.SourceLayer != model.LivePolicyLayerL2 &&
		input.SourceLayer != model.LivePolicyLayerL3 {
		writeError(w, http.StatusBadRequest, "请选择调教来源层")
		return
	}
	if input.Question == "" {
		writeError(w, http.StatusBadRequest, "原始问题不能为空")
		return
	}
	if input.FinalReply == "" && input.Feedback == "" && input.ObservedReply == "" {
		writeError(w, http.StatusBadRequest, "请至少提供真实回答、人工反馈或最终满意回复中的一项")
		return
	}
	if utf8.RuneCountInString(input.Question) > 4000 ||
		utf8.RuneCountInString(input.ObservedReply) > 6000 ||
		utf8.RuneCountInString(input.FinalReply) > 6000 ||
		utf8.RuneCountInString(input.Feedback) > 4000 ||
		utf8.RuneCountInString(input.SourceRef) > 512 {
		writeError(w, http.StatusBadRequest, "单次调教学习内容过长")
		return
	}

	var tenantID *int64
	if actor.Role == "customer" {
		if actor.TenantID == nil || input.RoomID <= 0 {
			writeError(w, http.StatusBadRequest, "客户调教学习必须指定自己的直播间")
			return
		}
		value := *actor.TenantID
		if _, err := s.getCoreRoomState(r.Context(), value, input.RoomID); err != nil {
			writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
			return
		}
		tenantID = &value
		input.SourceLayer = model.LivePolicyLayerL3
	} else {
		policyActor, viewOK := s.requireLivePolicyView(w, r)
		if !viewOK {
			return
		}
		actor = policyActor
	}

	learningInstruction := s.store.AgentPromptValue(r.Context(), "policy.learning.attribution", "从人工反馈中提炼可复用学习并严格返回 JSON。")
	prompt, err := buildLivePolicyLearningPrompt(learningInstruction, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "准备调教学习上下文失败")
		return
	}
	analysis, modelProvider, modelName, latencyMS, err := callPolicyLearningModel(
		r.Context(), prompt, input.LearningProvider, input.LearningModel,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "调教学习分析暂时不可用，请稍后重试")
		return
	}
	analysis = normalizeLivePolicyLearningModelOutput(analysis, input)
	if analysis.TargetLayer == model.LivePolicyLayerL3 && input.RoomID <= 0 {
		analysis.Reason += " 当前没有绑定具体直播间，采纳到用户层前需要指定直播间。"
	}
	item, err := s.store.CreateLivePolicyLearningCandidate(
		r.Context(),
		actor.UserID,
		tenantID,
		input,
		analysis.TargetLayer,
		analysis.Reason,
		analysis.RuleTitle,
		analysis.RuleText,
		analysis.ExecutionMode,
		modelProvider,
		modelName,
		analysis.AbsorbRecommended,
		analysis.Confidence,
		latencyMS,
		map[string]any{
			"promotion_level":  "candidate",
			"regression_cases": []string{},
		},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "保存调教学习候选失败")
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func buildLivePolicyLearningEvidencePrompt(instruction string, input model.CreateLivePolicyLearningEvidenceInput) (string, error) {
	historyRaw, err := json.Marshal(input.History)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(instruction + fmt.Sprintf(`

【本次证据】
证据类型：%s
证据来源：%s
来源：%s
行业：%s
直播间ID：%d
原始问题/场景：%s
当时真实回答：%s
人工修正后的回复：%s
用户反馈：%s
相关历史：%s
`, input.EvidenceType, input.SourceRef, input.SourceLayer, input.IndustryCode, input.RoomID, input.Question, input.ObservedReply, input.CorrectedReply, input.Feedback, string(historyRaw))), nil
}

func callPolicyLearningEvidenceModel(
	ctx context.Context,
	prompt string,
	provider string,
	modelName string,
) (livePolicyLearningEvidenceModelOutput, string, string, int64, error) {
	result, err := agentgateway.NewFromEnv().Complete(ctx, agentgateway.Request{
		Provider: provider,
		Model:    modelName,
		Messages: []agentgateway.Message{
			{Role: "system", Content: prompt},
			{Role: "user", Content: "请提炼本次证据中的学习候选并返回 JSON。"},
		},
		MaxTokens:      2400,
		EnableThinking: false,
		ResponseFormat: agentgateway.ResponseJSON,
		Timeout:        40 * time.Second,
	})
	if err != nil {
		return livePolicyLearningEvidenceModelOutput{}, "", "", 0, err
	}
	raw := stripPolicyJSONFence(result.Text)
	var output livePolicyLearningEvidenceModelOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return livePolicyLearningEvidenceModelOutput{}, result.Provider, result.Model, result.LatencyMS, fmt.Errorf("decode learning evidence output: %w", err)
	}
	output.Summary = strings.TrimSpace(output.Summary)
	if len(output.Proposals) > 4 {
		output.Proposals = output.Proposals[:4]
	}
	return output, result.Provider, result.Model, result.LatencyMS, nil
}

func normalizeLivePolicyLearningEvidenceProposal(
	proposal livePolicyLearningEvidenceProposal,
	input model.CreateLivePolicyLearningEvidenceInput,
) livePolicyLearningEvidenceProposal {
	legacyInput := model.CreateLivePolicyLearningCandidateInput{
		SourceLayer: input.SourceLayer,
		RoomID:      input.RoomID,
		FinalReply:  input.CorrectedReply,
		Feedback:    input.Feedback,
	}
	normalized := normalizeLivePolicyLearningModelOutput(livePolicyLearningModelOutput{
		AbsorbRecommended: proposal.AbsorbRecommended,
		TargetLayer:       proposal.TargetLayer,
		Reason:            proposal.Reason,
		Confidence:        proposal.Confidence,
		RuleTitle:         proposal.RuleTitle,
		RuleText:          proposal.RuleText,
		ExecutionMode:     proposal.ExecutionMode,
	}, legacyInput)
	proposal.AbsorbRecommended = normalized.AbsorbRecommended
	proposal.TargetLayer = normalized.TargetLayer
	proposal.Reason = normalized.Reason
	proposal.Confidence = normalized.Confidence
	proposal.RuleTitle = normalized.RuleTitle
	proposal.RuleText = normalized.RuleText
	proposal.ExecutionMode = normalized.ExecutionMode
	proposal.PromotionLevel = normalizeLearningPromotionLevel(proposal.PromotionLevel)
	proposal.RegressionCases = normalizeLearningRegressionCases(proposal.RegressionCases)
	return proposal
}

func (s *Server) livePolicyLearningCreateEvidence(w http.ResponseWriter, r *http.Request) {
	actor, ok := s.resolveActor(w, r)
	if !ok {
		return
	}
	var input model.CreateLivePolicyLearningEvidenceInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "学习证据格式错误")
		return
	}

	input.EvidenceType = normalizeLearningEvidenceType(input.EvidenceType)
	input.SourceRef = strings.TrimSpace(input.SourceRef)
	input.SourceLayer = strings.ToUpper(strings.TrimSpace(input.SourceLayer))
	input.IndustryCode = strings.ToLower(strings.TrimSpace(input.IndustryCode))
	input.Question = strings.TrimSpace(input.Question)
	input.ObservedReply = strings.TrimSpace(input.ObservedReply)
	input.CorrectedReply = strings.TrimSpace(input.CorrectedReply)
	input.Feedback = strings.TrimSpace(input.Feedback)
	input.LearningProvider = strings.TrimSpace(input.LearningProvider)
	input.LearningModel = strings.TrimSpace(input.LearningModel)
	input.History = normalizeLivePolicyLearningHistory(input.History)

	if input.SourceLayer == "" {
		if input.RoomID > 0 {
			input.SourceLayer = model.LivePolicyLayerL3
		} else {
			input.SourceLayer = model.LivePolicyLayerL1
		}
	}
	if input.SourceLayer != model.LivePolicyLayerL1 &&
		input.SourceLayer != model.LivePolicyLayerL2 &&
		input.SourceLayer != model.LivePolicyLayerL3 {
		writeError(w, http.StatusBadRequest, "来源层必须是规则层、行业层或用户层")
		return
	}
	if input.Question == "" && input.ObservedReply == "" &&
		input.CorrectedReply == "" && input.Feedback == "" {
		writeError(w, http.StatusBadRequest, "学习证据不能为空")
		return
	}
	if utf8.RuneCountInString(input.Question) > 4000 ||
		utf8.RuneCountInString(input.ObservedReply) > 6000 ||
		utf8.RuneCountInString(input.CorrectedReply) > 6000 ||
		utf8.RuneCountInString(input.Feedback) > 4000 ||
		utf8.RuneCountInString(input.SourceRef) > 512 {
		writeError(w, http.StatusBadRequest, "单次学习证据过长")
		return
	}

	var tenantID *int64
	if actor.Role == "customer" {
		if actor.TenantID == nil || input.RoomID <= 0 {
			writeError(w, http.StatusBadRequest, "客户学习必须指定自己的直播间")
			return
		}
		value := *actor.TenantID
		if _, err := s.getCoreRoomState(r.Context(), value, input.RoomID); err != nil {
			writeError(w, http.StatusNotFound, "直播间不存在或不属于当前终端")
			return
		}
		tenantID = &value
		input.SourceLayer = model.LivePolicyLayerL3
	} else {
		policyActor, viewOK := s.requireLivePolicyView(w, r)
		if !viewOK {
			return
		}
		actor = policyActor
	}

	evidenceInstruction := s.store.AgentPromptValue(r.Context(), "policy.learning.evidence", "从证据中提炼可审核的学习候选并严格返回 JSON。")
	prompt, err := buildLivePolicyLearningEvidencePrompt(evidenceInstruction, input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "准备学习证据失败")
		return
	}
	analysis, modelProvider, modelName, latencyMS, err := callPolicyLearningEvidenceModel(
		r.Context(), prompt, input.LearningProvider, input.LearningModel,
	)
	if err != nil {
		writeError(w, http.StatusBadGateway, "学习 Agent 暂时不可用，请稍后重试")
		return
	}

	items := make([]model.LivePolicyLearningCandidate, 0, len(analysis.Proposals))
	for index, proposal := range analysis.Proposals {
		proposal = normalizeLivePolicyLearningEvidenceProposal(proposal, input)
		if strings.TrimSpace(proposal.RuleText) == "" {
			continue
		}
		if proposal.TargetLayer == model.LivePolicyLayerL3 && input.RoomID <= 0 {
			proposal.Reason += " 当前没有绑定具体直播间，采纳到用户层前需要指定直播间。"
		}
		candidateInput := model.CreateLivePolicyLearningCandidateInput{
			SourceLayer:      input.SourceLayer,
			IndustryCode:     input.IndustryCode,
			RoomID:           input.RoomID,
			Question:         input.Question,
			ObservedReply:    input.ObservedReply,
			FinalReply:       input.CorrectedReply,
			Feedback:         input.Feedback,
			History:          input.History,
			EvidenceType:     input.EvidenceType,
			SourceRef:        input.SourceRef,
			LearningProvider: input.LearningProvider,
			LearningModel:    input.LearningModel,
		}
		item, createErr := s.store.CreateLivePolicyLearningCandidate(
			r.Context(),
			actor.UserID,
			tenantID,
			candidateInput,
			proposal.TargetLayer,
			proposal.Reason,
			proposal.RuleTitle,
			proposal.RuleText,
			proposal.ExecutionMode,
			modelProvider,
			modelName,
			proposal.AbsorbRecommended,
			proposal.Confidence,
			latencyMS,
			map[string]any{
				"summary":          analysis.Summary,
				"promotion_level":  proposal.PromotionLevel,
				"regression_cases": proposal.RegressionCases,
				"proposal_index":   index,
			},
		)
		if createErr != nil {
			writeError(w, http.StatusInternalServerError, "保存学习候选失败")
			return
		}
		items = append(items, item)
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"summary":        analysis.Summary,
		"items":          items,
		"model_provider": modelProvider,
		"model":          modelName,
		"latency_ms":     latencyMS,
	})
}

func (s *Server) livePolicyLearningListCandidates(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireLivePolicyView(w, r); !ok {
		return
	}
	status := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	if status == "" {
		status = model.LivePolicyLearningStatusPending
	}
	switch status {
	case "all",
		model.LivePolicyLearningStatusPending,
		model.LivePolicyLearningStatusAdopted,
		model.LivePolicyLearningStatusRejected:
	default:
		writeError(w, http.StatusBadRequest, "无效的学习状态")
		return
	}
	items, err := s.store.ListLivePolicyLearningCandidates(r.Context(), status, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取调教学习候选失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) livePolicyLearningRejectCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID, ok := namedPathID(w, r, "candidateID", "学习候选")
	if !ok {
		return
	}
	candidate, err := s.store.GetLivePolicyLearningCandidate(r.Context(), candidateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "学习候选不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取学习候选失败")
		return
	}
	requiredLayer := model.LivePolicyLayerL2
	if candidate.RecommendedLayer == model.LivePolicyLayerL1 {
		requiredLayer = model.LivePolicyLayerL1
	}
	actor, ok := s.requireLivePolicyLayerManage(w, r, requiredLayer)
	if !ok {
		return
	}
	var input model.RejectLivePolicyLearningCandidateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "拒绝原因格式错误")
		return
	}
	item, err := s.store.RejectLivePolicyLearningCandidate(
		r.Context(),
		candidateID,
		actor.UserID,
		input.ReviewNote,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "该学习候选已被处理")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func preferredLearningPolicyVersion(ctx model.LivePolicyContext) *model.LivePolicyVersion {
	for index := range ctx.Versions {
		if ctx.Versions[index].LifecycleStatus == "draft" {
			value := ctx.Versions[index]
			return &value
		}
	}
	if ctx.Active != nil {
		value := *ctx.Active
		return &value
	}
	return nil
}

func activeLearningPolicyVersion(ctx model.LivePolicyContext) *model.LivePolicyVersion {
	if ctx.Active != nil {
		value := *ctx.Active
		return &value
	}
	for index := range ctx.Versions {
		if strings.EqualFold(ctx.Versions[index].LifecycleStatus, "active") {
			value := ctx.Versions[index]
			return &value
		}
	}
	return nil
}

func answerReferenceLearningOverride(candidate model.LivePolicyLearningCandidate) (title, text, fixedText string) {
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(candidate.SourceRef)), "answer_reference:") {
		return strings.TrimSpace(candidate.RuleTitle), strings.TrimSpace(candidate.RuleText), ""
	}
	question := strings.TrimSpace(candidate.Question)
	finalReply := strings.TrimSpace(candidate.FinalReply)
	if finalReply == "" {
		finalReply = strings.TrimSpace(candidate.RuleText)
	}
	if question != "" {
		title = "回答参考：" + question
	} else {
		title = "回答参考"
	}
	if question == "" {
		text = "当前直播间已人工确认的回答事实/口径：" + finalReply + "。回答时必须保留其中的具体事实，不得用抽象方法论替代。"
	} else {
		text = fmt.Sprintf("当观众询问“%s”或语义相近的问题时，以人工确认的回答为事实口径：%s。允许按直播语气自然改写，但必须保留其中的商品年份、批次、产地、重量、物流等具体事实，不得改成抽象规则，也不得再回答为‘方案未注明’或‘无法确认’。", question, finalReply)
	}
	if candidate.ExecutionMode == model.LivePolicyModeVerbatim {
		fixedText = finalReply
	}
	return title, text, fixedText
}

func (s *Server) adoptLearningToAdminLayer(
	w http.ResponseWriter,
	r *http.Request,
	candidate model.LivePolicyLearningCandidate,
	input model.AdoptLivePolicyLearningCandidateInput,
	targetLayer string,
) (*model.LivePolicyVersion, model.Actor, bool) {
	actor, ok := s.requireLivePolicyLayerManage(w, r, targetLayer)
	if !ok {
		return nil, model.Actor{}, false
	}
	industryCode := strings.ToLower(strings.TrimSpace(input.IndustryCode))
	if industryCode == "" {
		industryCode = strings.ToLower(strings.TrimSpace(candidate.IndustryCode))
	}
	if targetLayer == model.LivePolicyLayerL2 && industryCode == "" {
		industryCode = "general"
	}
	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), targetLayer, industryCode, 0, 0, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusInternalServerError, "初始化目标策略层失败")
		return nil, model.Actor{}, false
	}
	contextValue, err := s.store.GetLivePolicyContext(
		r.Context(), targetLayer, industryCode, 0, 0,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取目标策略层失败")
		return nil, model.Actor{}, false
	}
	baseRules := []model.LivePolicyRule{}
	if err == nil {
		if base := preferredLearningPolicyVersion(contextValue); base != nil {
			baseRules = base.Rules
		}
	}
	mode := candidate.ExecutionMode
	if mode != model.LivePolicyModeVerbatim {
		mode = model.LivePolicyModeIntent
	}
	newRule := model.LivePolicyRule{
		Title:         candidate.RuleTitle,
		Text:          candidate.RuleText,
		ExecutionMode: mode,
		Enabled:       true,
	}
	if mode == model.LivePolicyModeVerbatim {
		newRule.FixedText = candidate.RuleText
	}
	rules := append(append([]model.LivePolicyRule{}, baseRules...), newRule)
	rules = policy.PrioritizeNewRules(
		policy.EnsureStableRuleKeys(targetLayer, rules, baseRules),
		baseRules,
	)
	var l1 *model.LivePolicyVersion
	if targetLayer == model.LivePolicyLayerL2 {
		l1, _ = s.store.GetActiveLivePolicyVersion(r.Context(), model.LivePolicyLayerL1, "", 0, 0)
	}
	draftInput := model.CreateLivePolicyDraftInput{
		Layer:        targetLayer,
		IndustryCode: industryCode,
		SourceText:   "调教学习候选 #" + fmt.Sprint(candidate.ID) + "：" + candidate.Feedback,
		Rules:        rules,
		Note:         "由调教学习中心采纳，原建议层：" + candidate.RecommendedLayer,
	}
	draftInput.Conflicts = policy.ValidateDraft(targetLayer, rules, nil, l1)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, draftInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成目标层草稿失败")
		return nil, model.Actor{}, false
	}
	return &item, actor, true
}

func (s *Server) adoptLearningToL3(
	w http.ResponseWriter,
	r *http.Request,
	candidate model.LivePolicyLearningCandidate,
	input model.AdoptLivePolicyLearningCandidateInput,
) (*model.LivePolicyVersion, model.Actor, bool) {
	roomID := input.RoomID
	if roomID <= 0 && candidate.RoomID != nil {
		roomID = *candidate.RoomID
	}
	if roomID <= 0 {
		writeError(w, http.StatusBadRequest, "采纳到用户层必须指定直播间")
		return nil, model.Actor{}, false
	}
	actor, tenantID, roomID, ok := s.requireCustomerPolicyRoomID(w, r, roomID)
	if !ok {
		return nil, model.Actor{}, false
	}
	if _, err := s.store.EnsureLivePolicyScope(
		r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID, actor.UserID, "",
	); err != nil {
		writeError(w, http.StatusInternalServerError, "初始化直播间用户层失败")
		return nil, model.Actor{}, false
	}
	contextValue, err := s.store.GetLivePolicyContext(
		r.Context(), model.LivePolicyLayerL3, "", tenantID, roomID,
	)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "读取直播间用户层失败")
		return nil, model.Actor{}, false
	}
	baseOverrides := []model.LivePolicyOverride{}
	if err == nil {
		// 用户层“采用”会立即发布，因此只继承当前 active 版本。
		// 这样不会把尚未发布的其他草稿顺带发布出去。
		if base := activeLearningPolicyVersion(contextValue); base != nil {
			baseOverrides = base.Overrides
		}
	}
	mode := candidate.ExecutionMode
	if mode != model.LivePolicyModeVerbatim {
		mode = model.LivePolicyModeIntent
	}
	overrideTitle, overrideText, answerReferenceFixedText := answerReferenceLearningOverride(candidate)
	override := model.LivePolicyOverride{
		Operation:     model.LivePolicyOverrideAdd,
		Title:         overrideTitle,
		Text:          overrideText,
		ExecutionMode: mode,
	}
	if mode == model.LivePolicyModeVerbatim {
		override.FixedText = answerReferenceFixedText
		if override.FixedText == "" {
			override.FixedText = strings.TrimSpace(candidate.RuleText)
		}
	}
	overrides := append(append([]model.LivePolicyOverride{}, baseOverrides...), override)
	overrides = policy.EnsureStableOverrideKeys(overrides, baseOverrides)
	_, l1, _, _, err := s.store.LoadLivePolicyLayers(r.Context(), tenantID, roomID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "读取直播间上层策略失败")
		return nil, model.Actor{}, false
	}
	draftInput := model.CreateLivePolicyDraftInput{
		Layer:      model.LivePolicyLayerL3,
		TenantID:   tenantID,
		RoomID:     roomID,
		SourceText: "调教学习候选 #" + fmt.Sprint(candidate.ID) + "：" + candidate.Feedback,
		Overrides:  overrides,
		Note:       "由调教学习中心采纳",
	}
	draftInput.Conflicts = policy.ValidateDraft(model.LivePolicyLayerL3, nil, overrides, l1)
	item, err := s.store.CreateLivePolicyDraft(r.Context(), actor.UserID, draftInput)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "生成直播间用户层草稿失败")
		return nil, model.Actor{}, false
	}
	item, err = s.store.PublishLivePolicyVersion(r.Context(), item.ID, actor.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "发布直播间用户层版本失败")
		return nil, model.Actor{}, false
	}
	if actor.IsInternalStaff() {
		_ = s.store.RegisterLiveSupportConfigVersion(
			r.Context(),
			item.ID,
			tenantID,
			roomID,
			actor.UserID,
			model.LiveSupportCapabilityL3Policy,
		)
	}
	return &item, actor, true
}

func (s *Server) livePolicyLearningAdoptCandidate(w http.ResponseWriter, r *http.Request) {
	candidateID, ok := namedPathID(w, r, "candidateID", "学习候选")
	if !ok {
		return
	}
	candidate, err := s.store.GetLivePolicyLearningCandidate(r.Context(), candidateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "学习候选不存在")
			return
		}
		writeError(w, http.StatusInternalServerError, "读取学习候选失败")
		return
	}
	if candidate.Status != model.LivePolicyLearningStatusPending {
		writeError(w, http.StatusConflict, "该学习候选已被处理")
		return
	}
	var input model.AdoptLivePolicyLearningCandidateInput
	if err := readJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "采纳参数格式错误")
		return
	}
	targetLayer := strings.ToUpper(strings.TrimSpace(input.TargetLayer))
	if targetLayer == "" {
		targetLayer = candidate.RecommendedLayer
	}
	if targetLayer != model.LivePolicyLayerL1 &&
		targetLayer != model.LivePolicyLayerL2 &&
		targetLayer != model.LivePolicyLayerL3 {
		writeError(w, http.StatusBadRequest, "目标层必须是规则层、行业层或用户层")
		return
	}
	if strings.TrimSpace(candidate.RuleText) == "" {
		writeError(w, http.StatusBadRequest, "学习候选没有可采纳的规则正文")
		return
	}

	var version *model.LivePolicyVersion
	var actor model.Actor
	var adopted bool
	if targetLayer == model.LivePolicyLayerL3 {
		version, actor, adopted = s.adoptLearningToL3(w, r, candidate, input)
	} else {
		version, actor, adopted = s.adoptLearningToAdminLayer(w, r, candidate, input, targetLayer)
	}
	if !adopted || version == nil {
		return
	}
	item, err := s.store.MarkLivePolicyLearningCandidateAdopted(
		r.Context(),
		candidate.ID,
		actor.UserID,
		version.ID,
		targetLayer,
		input.ReviewNote,
	)
	if err != nil {
		writeError(w, http.StatusConflict, "策略版本已生成，但学习候选状态更新失败，请刷新确认")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"candidate": item,
		"draft":     version,
		"version":   version,
		"published": targetLayer == model.LivePolicyLayerL3,
	})
}
